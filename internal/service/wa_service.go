package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "github.com/lib/pq"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	waCompanionReg "go.mau.fi/whatsmeow/proto/waCompanionReg"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waTypes "go.mau.fi/whatsmeow/types"
	waEvents "go.mau.fi/whatsmeow/types/events"

	"github.com/blesta/wa-reminder/internal/config"
	"github.com/blesta/wa-reminder/internal/domain/model"
	"github.com/blesta/wa-reminder/internal/repository/postgres"
	"github.com/rs/zerolog"
)

type WAService struct {
	cfg    *config.Config
	logger zerolog.Logger
	repo   *postgres.Repository

	mu          sync.RWMutex
	container   *sqlstore.Container
	client      *whatsmeow.Client
	qrCode      string
	qrExpiresAt time.Time
	qrCancel    context.CancelFunc

	qrRefreshInProgress  bool
	lastQRRefreshAttempt time.Time
}

var ErrQRCodeUnavailable = errors.New("qr code unavailable")

func NewWAService(ctx context.Context, cfg *config.Config, logger zerolog.Logger, repo *postgres.Repository) *WAService {
	return &WAService{
		cfg:    cfg,
		logger: logger,
		repo:   repo,
	}
}

func (s *WAService) Start(ctx context.Context) error {
	client, err := s.createClient(ctx)
	if err != nil {
		return err
	}

	if err := s.repo.UpdateWAStatus(ctx, model.WAStatusConnecting, nil, nil); err != nil {
		s.logger.Warn().Err(err).Msg("failed to set WA status connecting")
	}

	if client.Store.ID == nil {
		if err := s.startQRListening(client); err != nil {
			return err
		}
		if err := s.repo.UpdateWAStatus(ctx, model.WAStatusNeedQR, nil, nil); err != nil {
			s.logger.Warn().Err(err).Msg("failed to set WA status need_qr")
		}
	}

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect whatsapp client: %w", err)
	}
	return nil
}

func (s *WAService) ensureContainer(ctx context.Context) (*sqlstore.Container, error) {
	s.mu.RLock()
	container := s.container
	s.mu.RUnlock()
	if container != nil {
		return container, nil
	}

	dbLog := waLog.Stdout("Database", "WARN", true)
	newContainer, err := sqlstore.New(ctx, "postgres", s.cfg.DatabaseURL, dbLog)
	if err != nil {
		return nil, fmt.Errorf("init whatsmeow sql store: %w", err)
	}

	s.mu.Lock()
	if s.container == nil {
		s.container = newContainer
	}
	container = s.container
	s.mu.Unlock()
	return container, nil
}

func (s *WAService) configureCompanionIdentity() {
	// Override companion identity to avoid "Other Device" label in Linked Devices.
	waStore.SetOSInfo("Windows", waStore.GetWAVersion())
	waStore.DeviceProps.PlatformType = waCompanionReg.DeviceProps_CHROME.Enum()
	waStore.DeviceProps.RequireFullSync = proto.Bool(true)
}

func (s *WAService) createClient(ctx context.Context) (*whatsmeow.Client, error) {
	container, err := s.ensureContainer(ctx)
	if err != nil {
		return nil, err
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("get first device store: %w", err)
	}

	s.configureCompanionIdentity()

	clientLog := waLog.Stdout("Client", "WARN", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(s.handleEvent)

	s.mu.Lock()
	s.client = client
	s.mu.Unlock()
	return client, nil
}

func (s *WAService) startQRListening(client *whatsmeow.Client) error {
	s.mu.Lock()
	if s.qrCancel != nil {
		s.qrCancel()
		s.qrCancel = nil
	}
	s.qrCode = ""
	s.qrExpiresAt = time.Time{}
	s.mu.Unlock()

	qrCtx, cancel := context.WithCancel(context.Background())
	qrChan, err := client.GetQRChannel(qrCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("get QR channel: %w", err)
	}

	s.mu.Lock()
	s.qrCancel = cancel
	s.mu.Unlock()
	go s.handleQRChannel(qrCtx, qrChan)
	return nil
}

func (s *WAService) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client != nil && s.client.IsConnected()
}

func (s *WAService) GetStatus(ctx context.Context) (*model.WASession, error) {
	return s.repo.GetWAStatus(ctx)
}

func (s *WAService) GetQR(ctx context.Context) (*model.WAQRCode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.qrCode == "" {
		return nil, ErrQRCodeUnavailable
	}
	remaining := int(time.Until(s.qrExpiresAt).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	return &model.WAQRCode{
		QRCode:           s.qrCode,
		ExpiresInSeconds: remaining,
	}, nil
}

func (s *WAService) Reconnect(ctx context.Context) error {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("client not initialized")
	}
	if client.IsConnected() {
		client.Disconnect()
	}
	if err := s.repo.UpdateWAStatus(ctx, model.WAStatusConnecting, nil, nil); err != nil {
		s.logger.Warn().Err(err).Msg("failed to set reconnect state")
	}
	return client.Connect()
}

func (s *WAService) RefreshQR(ctx context.Context) error {
	s.mu.Lock()
	// If a QR already exists and still valid, don't churn websocket/session.
	if s.qrCode != "" && time.Until(s.qrExpiresAt) > 8*time.Second {
		s.mu.Unlock()
		return nil
	}
	// Deduplicate concurrent refresh requests.
	if s.qrRefreshInProgress {
		s.mu.Unlock()
		return nil
	}
	s.qrRefreshInProgress = true
	s.lastQRRefreshAttempt = time.Now()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.qrRefreshInProgress = false
		s.mu.Unlock()
	}()

	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()
	if client == nil || client.Store.Deleted {
		var err error
		client, err = s.createClient(ctx)
		if err != nil {
			return err
		}
	}

	var lastDeletedErr error
	for attempt := 0; attempt < 3; attempt++ {
		if client.Store.ID != nil {
			// If DB/session status says need_qr but store still has an ID and client isn't logged in,
			// the local store is stale (e.g. external logout). Recreate it automatically.
			if !client.IsLoggedIn() {
				client.Disconnect()
				if client.Store.ID != nil && !client.Store.Deleted {
					if err := client.Store.Delete(ctx); err != nil {
						return fmt.Errorf("clear stale wa store before qr refresh: %w", err)
					}
				}
				var err error
				client, err = s.createClient(ctx)
				if err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("device already paired")
		}

		if err := s.startQRListening(client); err != nil {
			if errors.Is(err, waStore.ErrDeviceDeleted) {
				lastDeletedErr = err
				var err error
				client, err = s.createClient(ctx)
				if err != nil {
					return err
				}
				continue
			}
			return err
		}

		if client.IsConnected() {
			client.Disconnect()
		}
		if err := s.repo.UpdateWAStatus(ctx, model.WAStatusNeedQR, nil, nil); err != nil {
			s.logger.Warn().Err(err).Msg("failed to set WA status need_qr")
		}

		if err := client.Connect(); err != nil {
			if errors.Is(err, waStore.ErrDeviceDeleted) {
				lastDeletedErr = err
				client, err = s.createClient(ctx)
				if err != nil {
					return err
				}
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("refresh qr failed: %w", lastDeletedErr)
}

func (s *WAService) Logout(ctx context.Context) error {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("client not initialized")
	}
	logoutErr := client.Logout(ctx)
	if logoutErr != nil && !errors.Is(logoutErr, whatsmeow.ErrNotLoggedIn) {
		return logoutErr
	}

	// External logout can cause ErrNotLoggedIn while local store is still present.
	// Ensure stale local store is removed so QR re-pair can start cleanly.
	if errors.Is(logoutErr, whatsmeow.ErrNotLoggedIn) {
		client.Disconnect()
		if client.Store.ID != nil && !client.Store.Deleted {
			if err := client.Store.Delete(ctx); err != nil {
				return fmt.Errorf("clear stale local store after not-logged-in logout: %w", err)
			}
		}
	}
	if err := s.repo.ClearWASession(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	s.qrCode = ""
	s.qrExpiresAt = time.Time{}
	s.mu.Unlock()

	if _, err := s.createClient(ctx); err != nil {
		return fmt.Errorf("reinitialize wa client after logout: %w", err)
	}
	return nil
}

func (s *WAService) CheckPhoneOnWhatsApp(ctx context.Context, phone string) (bool, *string, error) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()
	if client == nil || !client.IsConnected() {
		return false, nil, fmt.Errorf("wa client not connected")
	}
	resp, err := client.IsOnWhatsApp(ctx, []string{phone})
	if err != nil {
		return false, nil, err
	}
	if len(resp) == 0 || !resp[0].IsIn {
		return false, nil, nil
	}
	jid := resp[0].JID.String()
	return true, &jid, nil
}

func (s *WAService) SendTextWithTyping(ctx context.Context, jid waTypes.JID, message string, typingDuration time.Duration) (string, error) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()
	if client == nil || !client.IsConnected() {
		return "", fmt.Errorf("wa client not connected")
	}

	_ = client.SendPresence(ctx, waTypes.PresenceAvailable)
	_ = client.SendChatPresence(ctx, jid, waTypes.ChatPresenceComposing, waTypes.ChatPresenceMediaText)
	defer func() {
		_ = client.SendChatPresence(context.Background(), jid, waTypes.ChatPresencePaused, waTypes.ChatPresenceMediaText)
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(typingDuration):
	}

	resp, err := client.SendMessage(ctx, jid, &waProto.Message{
		Conversation: &message,
	})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (s *WAService) handleQRChannel(ctx context.Context, qrChan <-chan whatsmeow.QRChannelItem) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-qrChan:
			if !ok {
				return
			}
			if evt.Event == "code" {
				s.mu.Lock()
				s.qrCode = evt.Code
				s.qrExpiresAt = time.Now().Add(45 * time.Second)
				s.mu.Unlock()
			} else if evt.Event == "timeout" {
				s.mu.Lock()
				s.qrCode = ""
				s.qrExpiresAt = time.Time{}
				s.mu.Unlock()
			}
		}
	}
}

func (s *WAService) handleEvent(evt interface{}) {
	ctx := context.Background()
	switch v := evt.(type) {
	case *waEvents.Connected:
		_ = s.repo.UpdateWAStatus(ctx, model.WAStatusConnected, nil, nil)
	case *waEvents.Disconnected:
		_ = s.repo.UpdateWAStatus(ctx, model.WAStatusDisconnected, nil, nil)
	case *waEvents.LoggedOut:
		_ = s.repo.UpdateWAStatus(ctx, model.WAStatusNeedQR, nil, nil)
	case *waEvents.PairSuccess:
		jid := v.ID.String()
		masked := maskPhone(v.ID.User)
		_ = s.repo.UpdateWAStatus(ctx, model.WAStatusConnected, &masked, &jid)
	}
}

func maskPhone(phone string) string {
	if len(phone) <= 4 {
		return phone
	}
	return phone[:4] + "******" + phone[len(phone)-2:]
}

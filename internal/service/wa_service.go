package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	_ "github.com/lib/pq"
	waLog "go.mau.fi/whatsmeow/util/log"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
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
	client      *whatsmeow.Client
	qrCode      string
	qrExpiresAt time.Time
}

func NewWAService(ctx context.Context, cfg *config.Config, logger zerolog.Logger, repo *postgres.Repository) *WAService {
	return &WAService{
		cfg:    cfg,
		logger: logger,
		repo:   repo,
	}
}

func (s *WAService) Start(ctx context.Context) error {
	dbLog := waLog.Stdout("Database", "WARN", true)
	container, err := sqlstore.New(ctx, "postgres", s.cfg.DatabaseURL, dbLog)
	if err != nil {
		return fmt.Errorf("init whatsmeow sql store: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("get first device store: %w", err)
	}

	clientLog := waLog.Stdout("Client", "WARN", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(s.handleEvent)

	s.mu.Lock()
	s.client = client
	s.mu.Unlock()

	if err := s.repo.UpdateWAStatus(ctx, model.WAStatusConnecting, nil, nil); err != nil {
		s.logger.Warn().Err(err).Msg("failed to set WA status connecting")
	}

	if client.Store.ID == nil {
		qrChan, err := client.GetQRChannel(ctx)
		if err != nil {
			return fmt.Errorf("get QR channel: %w", err)
		}
		go s.handleQRChannel(ctx, qrChan)
		if err := s.repo.UpdateWAStatus(ctx, model.WAStatusNeedQR, nil, nil); err != nil {
			s.logger.Warn().Err(err).Msg("failed to set WA status need_qr")
		}
	}

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect whatsapp client: %w", err)
	}
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
		return nil, fmt.Errorf("qr code unavailable")
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

func (s *WAService) Logout(ctx context.Context) error {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("client not initialized")
	}
	if err := client.Logout(ctx); err != nil {
		return err
	}
	if err := s.repo.ClearWASession(ctx); err != nil {
		return err
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

(function (global) {
  "use strict";
  const QR_MANUAL_REFRESH_COOLDOWN_MS = 3000;

  function createStateStore(initial) {
    let state = { ...initial };
    return {
      get: () => ({ ...state }),
      set: (patch) => {
        state = { ...state, ...patch };
        return { ...state };
      },
    };
  }

  function serializeQuery(params) {
    const query = new URLSearchParams();
    Object.keys(params).forEach((key) => {
      const value = params[key];
      if (value !== undefined && value !== null && value !== "") {
        query.set(key, String(value));
      }
    });
    return query.toString();
  }

  function buildExportURL(filters) {
    const query = serializeQuery(filters);
    return "/admin-api/v1/deliveries/export.csv" + (query ? "?" + query : "");
  }

  function formatDateTime(value) {
    if (!value) return "-";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "-";
    return d.toLocaleString();
  }

  function debounce(fn, wait) {
    let t = null;
    return function debounced() {
      const ctx = this;
      const args = arguments;
      clearTimeout(t);
      t = setTimeout(() => fn.apply(ctx, args), wait);
    };
  }

  function apiClient(base) {
    const request = async (path, options) => {
      const res = await fetch(base + path, options);
      if (!res.ok) {
        let payload;
        try {
          payload = await res.json();
        } catch (e) {
          payload = null;
        }
        throw new Error((payload && payload.error && payload.error.message) || "request failed");
      }
      return res.json();
    };
    return {
      get: (path) => request(path, { method: "GET" }),
      post: (path) => request(path, { method: "POST" }),
      put: (path, body) =>
        request(path, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body || {}),
        }),
    };
  }

  function createDashboard() {
    const dom = {
      waStatusBadge: document.getElementById("waStatusBadge"),
      waPhone: document.getElementById("waPhone"),
      waLastSeen: document.getElementById("waLastSeen"),
      qrSection: document.getElementById("qrSection"),
      qrCodeView: document.getElementById("qrCodeView"),
      qrExpires: document.getElementById("qrExpires"),
      qrManualHint: document.getElementById("qrManualHint"),
      filterRange: document.getElementById("filterRange"),
      filterStatus: document.getElementById("filterStatus"),
      filterSearch: document.getElementById("filterSearch"),
      filterFrom: document.getElementById("filterFrom"),
      filterTo: document.getElementById("filterTo"),
      btnApplyFilters: document.getElementById("btnApplyFilters"),
      btnRefreshDeliveries: document.getElementById("btnRefreshDeliveries"),
      btnExportCSV: document.getElementById("btnExportCSV"),
      btnReconnect: document.getElementById("btnReconnect"),
      btnQueueToggle: document.getElementById("btnQueueToggle"),
      btnLogout: document.getElementById("btnLogout"),
      btnRefreshQR: document.getElementById("btnRefreshQR"),
      tableBody: document.getElementById("deliveryTableBody"),
      deliveryMeta: document.getElementById("deliveryMeta"),
      paginationLimit: document.getElementById("paginationLimit"),
      btnPrevPage: document.getElementById("btnPrevPage"),
      btnNextPage: document.getElementById("btnNextPage"),
      statQueued: document.getElementById("statQueued"),
      statProcessing: document.getElementById("statProcessing"),
      statSent: document.getElementById("statSent"),
      statRetrying: document.getElementById("statRetrying"),
      statFailed: document.getElementById("statFailed"),
      statSuccessRate: document.getElementById("statSuccessRate"),
      confirmText: document.getElementById("confirmText"),
      confirmSubmit: document.getElementById("confirmSubmit"),
      detailPayload: document.getElementById("detailPayload"),
      toastMessage: document.getElementById("toastMessage"),
      appToast: document.getElementById("appToast"),
      templateAcceptedPlaceholders: document.getElementById("templateAcceptedPlaceholders"),
      templateH30: document.getElementById("templateH30"),
      templateH15: document.getElementById("templateH15"),
      templateH7: document.getElementById("templateH7"),
      templateH30UpdatedAt: document.getElementById("templateH30UpdatedAt"),
      templateH15UpdatedAt: document.getElementById("templateH15UpdatedAt"),
      templateH7UpdatedAt: document.getElementById("templateH7UpdatedAt"),
      btnSaveTemplateH30: document.getElementById("btnSaveTemplateH30"),
      btnSaveTemplateH15: document.getElementById("btnSaveTemplateH15"),
      btnSaveTemplateH7: document.getElementById("btnSaveTemplateH7"),
    };

    const api = apiClient("");
    const store = createStateStore({
      status: "",
      page: 1,
      limit: 20,
      total: 0,
      range: "today",
      filters: { status: "", search: "", from: "", to: "" },
      queuePaused: false,
      loading: false,
      confirmAction: null,
      lastQRRefreshAt: 0,
      qrRefreshInFlight: false,
      qrVisibleByUser: false,
    });

    const confirmModal = new bootstrap.Modal(document.getElementById("confirmModal"));
    const detailModal = new bootstrap.Modal(document.getElementById("detailModal"));
    const toast = new bootstrap.Toast(dom.appToast);
    let qrRenderer = null;

    function showToast(message) {
      dom.toastMessage.textContent = message;
      toast.show();
    }

    function setActionButtonsDisabled(disabled) {
      [dom.btnReconnect, dom.btnQueueToggle, dom.btnLogout, dom.btnExportCSV].forEach((btn) => {
        btn.disabled = disabled;
      });
    }

    function getTemplateBindings(code) {
      if (code === "expiry_h30") return { textarea: dom.templateH30, updatedAt: dom.templateH30UpdatedAt, button: dom.btnSaveTemplateH30 };
      if (code === "expiry_h15") return { textarea: dom.templateH15, updatedAt: dom.templateH15UpdatedAt, button: dom.btnSaveTemplateH15 };
      if (code === "expiry_h7") return { textarea: dom.templateH7, updatedAt: dom.templateH7UpdatedAt, button: dom.btnSaveTemplateH7 };
      return null;
    }

    function renderReminderTemplates(payload) {
      const items = (payload && payload.items) || [];
      const placeholders = (payload && payload.accepted_placeholders) || [];
      if (dom.templateAcceptedPlaceholders) {
        dom.templateAcceptedPlaceholders.textContent =
          "Accepted placeholders: " + (placeholders.length ? placeholders.join(", ") : "{{customer_name}}, {{service_name}}, {{expired_date}}");
      }
      items.forEach((item) => {
        const binding = getTemplateBindings(item.template_code);
        if (!binding) return;
        binding.textarea.value = item.message_template || "";
        binding.updatedAt.textContent = "updated_at: " + formatDateTime(item.updated_at);
      });
    }

    function renderStatus(data) {
      const current = store.get();
      const prevStatus = current.status;
      const status = (data.status || "unknown").toLowerCase();
      store.set({ status: status });
      dom.waStatusBadge.textContent = status;
      dom.waStatusBadge.className = "badge";
      if (status === "connected") dom.waStatusBadge.classList.add("text-bg-success");
      else if (status === "need_qr") dom.waStatusBadge.classList.add("text-bg-warning");
      else dom.waStatusBadge.classList.add("text-bg-secondary");

      dom.waPhone.textContent = data.phone_masked || "-";
      dom.waLastSeen.textContent = formatDateTime(data.last_seen_at);
      dom.qrSection.classList.toggle("d-none", status !== "need_qr");

      // Any transition into need_qr (including external logout from mobile) forces manual QR mode.
      if (status === "need_qr" && prevStatus !== "need_qr") {
        store.set({ qrVisibleByUser: false, qrRefreshInFlight: false });
        setManualQRMode(true);
      }

      if (status !== "need_qr") {
        dom.qrCodeView.innerHTML = "";
        dom.qrExpires.textContent = "-";
        qrRenderer = null;
        store.set({ qrVisibleByUser: false, qrRefreshInFlight: false });
        setManualQRMode(false);
      }
    }

    function renderQRCode(rawCode) {
      if (!rawCode) {
        dom.qrCodeView.innerHTML = "";
        return;
      }
      dom.qrCodeView.innerHTML = "";
      qrRenderer = new QRCode(dom.qrCodeView, {
        text: rawCode,
        width: 220,
        height: 220,
        correctLevel: QRCode.CorrectLevel.M,
      });
    }

    function isQRUnavailableError(err) {
      const msg = String((err && err.message) || "").toLowerCase();
      return msg.includes("qr code unavailable");
    }

    function setManualQRMode(enabled) {
      dom.qrCodeView.classList.toggle("d-none", enabled);
      dom.qrManualHint.classList.toggle("d-none", !enabled);
      if (enabled) {
        dom.qrCodeView.innerHTML = "";
        dom.qrExpires.textContent = "-";
        qrRenderer = null;
      }
    }

    function renderStats(data) {
      dom.statQueued.textContent = data.queued || 0;
      dom.statProcessing.textContent = data.processing || 0;
      dom.statSent.textContent = data.sent || 0;
      dom.statRetrying.textContent = data.retrying || 0;
      dom.statFailed.textContent = data.failed || 0;
      dom.statSuccessRate.textContent = (data.success_rate || 0).toFixed(2) + "%";
    }

    function renderDeliveries(payload) {
      const items = payload.items || [];
      const meta = payload.meta || {};
      dom.tableBody.innerHTML = "";
      if (!items.length) {
        dom.tableBody.innerHTML = '<tr><td colspan="7" class="text-center text-muted">No delivery data</td></tr>';
      } else {
        items.forEach((it) => {
          const tr = document.createElement("tr");
          tr.innerHTML = [
            "<td>" + formatDateTime(it.created_at) + "</td>",
            "<td>" + (it.phone || "-") + "</td>",
            "<td>" + (it.service_name || "-") + "</td>",
            "<td><span class='badge text-bg-light border'>" + (it.status || "-") + "</span></td>",
            "<td>" + (it.attempt_no || 0) + "</td>",
            "<td>" + ((it.error_code || "") + " " + (it.error_message || "")).trim() + "</td>",
            "<td><button class='btn btn-sm btn-outline-primary' data-delivery-id='" + it.id + "'>View</button></td>",
          ].join("");
          dom.tableBody.appendChild(tr);
        });
      }
      const page = meta.page || 1;
      const limit = meta.limit || 20;
      const total = meta.total || 0;
      store.set({ page: page, limit: limit, total: total });
      dom.deliveryMeta.textContent = "Page " + page + " | Limit " + limit + " | Total " + total;
    }

    async function fetchStatusAndQR() {
      try {
        const statusRes = await api.get("/admin-api/v1/wa/status");
        renderStatus(statusRes.data);
        if ((statusRes.data.status || "").toLowerCase() === "need_qr") {
          const state = store.get();
          if (!state.qrVisibleByUser) {
            setManualQRMode(true);
            return;
          }

          setManualQRMode(false);
          let qrCode = "";
          let expires = 0;
          try {
            const qrRes = await api.get("/admin-api/v1/wa/qr");
            qrCode = qrRes.data.qr_code || "";
            expires = Number(qrRes.data.expires_in_seconds || 0);
          } catch (err) {
            if (!isQRUnavailableError(err)) {
              throw err;
            }
          }
          renderQRCode(qrCode);
          dom.qrExpires.textContent = expires;
        } else {
          store.set({ qrRefreshInFlight: false });
          setManualQRMode(false);
          dom.qrExpires.textContent = "-";
        }
      } catch (err) {
        showToast("Failed loading WA status: " + err.message);
      }
    }

    async function refreshQR(manual) {
      const state = store.get();
      const now = Date.now();
      if (state.qrRefreshInFlight) return;
      if (now - state.lastQRRefreshAt < QR_MANUAL_REFRESH_COOLDOWN_MS) return;

      try {
        store.set({
          lastQRRefreshAt: now,
          qrRefreshInFlight: true,
          qrVisibleByUser: true,
        });
        setManualQRMode(false);
        await api.post("/admin-api/v1/wa/qr/refresh");
        if (manual) {
          showToast("QR refresh triggered");
        }
        setTimeout(fetchStatusAndQR, 1200);
      } catch (err) {
        if (manual) {
          showToast("Failed refreshing QR: " + err.message);
        }
      } finally {
        store.set({ qrRefreshInFlight: false });
      }
    }

    async function fetchStats() {
      const state = store.get();
      try {
        const res = await api.get("/admin-api/v1/stats/overview?" + serializeQuery({ range: state.range }));
        renderStats(res.data);
      } catch (err) {
        showToast("Failed loading stats: " + err.message);
      }
    }

    async function fetchDeliveries() {
      const state = store.get();
      try {
        const res = await api.get("/admin-api/v1/deliveries?" + serializeQuery({
          page: state.page,
          limit: state.limit,
          status: state.filters.status,
          search: state.filters.search,
          from: state.filters.from,
          to: state.filters.to,
        }));
        renderDeliveries(res.data);
      } catch (err) {
        showToast("Failed loading deliveries: " + err.message);
      }
    }

    async function fetchReminderTemplates() {
      try {
        const res = await api.get("/admin-api/v1/reminder-templates");
        renderReminderTemplates(res.data);
      } catch (err) {
        showToast("Failed loading reminder templates: " + err.message);
      }
    }

    async function saveTemplate(code) {
      const binding = getTemplateBindings(code);
      if (!binding) return;
      const value = binding.textarea.value.trim();
      if (!value) {
        showToast("Template tidak boleh kosong");
        return;
      }
      binding.button.disabled = true;
      try {
        await api.put("/admin-api/v1/reminder-templates/" + code, { message_template: value });
        showToast("Template " + code + " berhasil disimpan");
        await fetchReminderTemplates();
      } catch (err) {
        showToast("Failed saving template: " + err.message);
      } finally {
        binding.button.disabled = false;
      }
    }

    async function fetchDeliveryDetail(id) {
      try {
        const res = await api.get("/admin-api/v1/deliveries/" + id);
        dom.detailPayload.textContent = JSON.stringify(res.data, null, 2);
        detailModal.show();
      } catch (err) {
        showToast("Failed loading detail: " + err.message);
      }
    }

    function bindEvents() {
      dom.btnApplyFilters.addEventListener("click", () => {
        const nextFilters = {
          status: dom.filterStatus.value,
          search: dom.filterSearch.value.trim(),
          from: dom.filterFrom.value,
          to: dom.filterTo.value,
        };
        store.set({ filters: nextFilters, page: 1, range: dom.filterRange.value });
        fetchStats();
        fetchDeliveries();
      });

      dom.filterSearch.addEventListener("input", debounce(() => {
        const state = store.get();
        store.set({
          filters: { ...state.filters, search: dom.filterSearch.value.trim() },
          page: 1,
        });
        fetchDeliveries();
      }, 400));

      dom.btnRefreshDeliveries.addEventListener("click", fetchDeliveries);
      dom.btnRefreshQR.addEventListener("click", () => refreshQR(true));
      dom.paginationLimit.addEventListener("change", () => {
        store.set({ limit: Number(dom.paginationLimit.value), page: 1 });
        fetchDeliveries();
      });
      dom.btnPrevPage.addEventListener("click", () => {
        const state = store.get();
        if (state.page > 1) {
          store.set({ page: state.page - 1 });
          fetchDeliveries();
        }
      });
      dom.btnNextPage.addEventListener("click", () => {
        const state = store.get();
        const maxPage = Math.max(1, Math.ceil(state.total / state.limit));
        if (state.page < maxPage) {
          store.set({ page: state.page + 1 });
          fetchDeliveries();
        }
      });

      dom.tableBody.addEventListener("click", (evt) => {
        const target = evt.target;
        if (!(target instanceof HTMLElement)) return;
        const id = target.getAttribute("data-delivery-id");
        if (id) fetchDeliveryDetail(id);
      });

      dom.btnExportCSV.addEventListener("click", () => {
        const state = store.get();
        const href = buildExportURL({
          status: state.filters.status,
          search: state.filters.search,
          from: state.filters.from,
          to: state.filters.to,
        });
        window.location.href = href;
      });
      dom.btnSaveTemplateH30.addEventListener("click", () => saveTemplate("expiry_h30"));
      dom.btnSaveTemplateH15.addEventListener("click", () => saveTemplate("expiry_h15"));
      dom.btnSaveTemplateH7.addEventListener("click", () => saveTemplate("expiry_h7"));

      dom.btnReconnect.addEventListener("click", () => askConfirm("Reconnect WhatsApp connection?", async () => {
        await api.post("/admin-api/v1/wa/reconnect");
        showToast("Reconnect triggered");
        setManualQRMode(false);
        fetchStatusAndQR();
      }));
      dom.btnLogout.addEventListener("click", () => askConfirm("Logout active WA session?", async () => {
        await api.post("/admin-api/v1/wa/logout");
        store.set({ qrVisibleByUser: false });
        setManualQRMode(true);
        showToast("Logout completed");
        fetchStatusAndQR();
      }));
      dom.btnQueueToggle.addEventListener("click", () => {
        const state = store.get();
        const pause = !state.queuePaused;
        askConfirm((pause ? "Pause" : "Resume") + " queue now?", async () => {
          await api.post("/admin-api/v1/queue/" + (pause ? "pause" : "resume"));
          store.set({ queuePaused: pause });
          dom.btnQueueToggle.textContent = pause ? "Resume Queue" : "Pause Queue";
          showToast("Queue " + (pause ? "paused" : "resumed"));
        });
      });
      dom.confirmSubmit.addEventListener("click", async () => {
        const action = store.get().confirmAction;
        if (!action) return;
        try {
          setActionButtonsDisabled(true);
          await action();
        } catch (err) {
          showToast("Action failed: " + err.message);
        } finally {
          setActionButtonsDisabled(false);
          store.set({ confirmAction: null });
          confirmModal.hide();
        }
      });
    }

    function askConfirm(text, action) {
      dom.confirmText.textContent = text;
      store.set({ confirmAction: action });
      confirmModal.show();
    }

    function startPolling() {
      fetchStatusAndQR();
      fetchStats();
      fetchDeliveries();
      fetchReminderTemplates();
      setInterval(fetchStatusAndQR, 5000);
      setInterval(fetchStats, 12000);
    }

    bindEvents();
    startPolling();
  }

  if (typeof window !== "undefined" && typeof document !== "undefined") {
    document.addEventListener("DOMContentLoaded", createDashboard);
  }

  const exported = { createStateStore, serializeQuery, buildExportURL, formatDateTime, debounce };
  if (typeof module !== "undefined" && module.exports) {
    module.exports = exported;
  } else {
    global.WADashboard = exported;
  }
})(typeof globalThis !== "undefined" ? globalThis : this);

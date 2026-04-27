(function (global) {
  "use strict";

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

    function renderStatus(data) {
      const status = (data.status || "unknown").toLowerCase();
      dom.waStatusBadge.textContent = status;
      dom.waStatusBadge.className = "badge";
      if (status === "connected") dom.waStatusBadge.classList.add("text-bg-success");
      else if (status === "need_qr") dom.waStatusBadge.classList.add("text-bg-warning");
      else dom.waStatusBadge.classList.add("text-bg-secondary");

      dom.waPhone.textContent = data.phone_masked || "-";
      dom.waLastSeen.textContent = formatDateTime(data.last_seen_at);
      dom.qrSection.classList.toggle("d-none", status !== "need_qr");
      if (status !== "need_qr") {
        dom.qrCodeView.innerHTML = "";
        qrRenderer = null;
      }
    }

    function renderQRCode(rawCode) {
      if (!rawCode) {
        dom.qrCodeView.innerHTML = "-";
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
          const qrRes = await api.get("/admin-api/v1/wa/qr");
          renderQRCode(qrRes.data.qr_code || "");
          dom.qrExpires.textContent = qrRes.data.expires_in_seconds || 0;
        }
      } catch (err) {
        showToast("Failed loading WA status: " + err.message);
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
      dom.btnRefreshQR.addEventListener("click", fetchStatusAndQR);
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

      dom.btnReconnect.addEventListener("click", () => askConfirm("Reconnect WhatsApp connection?", async () => {
        await api.post("/admin-api/v1/wa/reconnect");
        showToast("Reconnect triggered");
        fetchStatusAndQR();
      }));
      dom.btnLogout.addEventListener("click", () => askConfirm("Logout active WA session?", async () => {
        await api.post("/admin-api/v1/wa/logout");
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
      setInterval(fetchStatusAndQR, 3000);
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

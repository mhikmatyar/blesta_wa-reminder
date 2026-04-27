const test = require("node:test");
const assert = require("node:assert/strict");

const {
  createStateStore,
  serializeQuery,
  buildExportURL,
  formatDateTime,
  debounce,
} = require("./app.js");

test("serializeQuery should skip empty values", () => {
  const query = serializeQuery({
    status: "sent",
    search: "",
    from: "2026-04-27",
    page: 1,
  });
  assert.equal(query, "status=sent&from=2026-04-27&page=1");
});

test("buildExportURL should construct export endpoint", () => {
  const url = buildExportURL({ status: "send_failed", from: "2026-04-01" });
  assert.equal(url, "/admin-api/v1/deliveries/export.csv?status=send_failed&from=2026-04-01");
});

test("createStateStore should provide immutable snapshots", () => {
  const store = createStateStore({ page: 1, loading: false });
  const first = store.get();
  first.page = 99;
  assert.equal(store.get().page, 1);
  const second = store.set({ page: 2 });
  assert.equal(second.page, 2);
});

test("formatDateTime should handle invalid and valid values", () => {
  assert.equal(formatDateTime(null), "-");
  assert.equal(formatDateTime("not-a-date"), "-");
  assert.notEqual(formatDateTime("2026-04-27T10:00:00Z"), "-");
});

test("debounce should execute once", async () => {
  let n = 0;
  const fn = debounce(() => {
    n += 1;
  }, 30);

  fn();
  fn();
  fn();
  await new Promise((r) => setTimeout(r, 50));
  assert.equal(n, 1);
});

# Frontend Test Setup

Frontend test menggunakan Node.js built-in test runner agar ringan dan tidak menambah tooling besar.

## Prasyarat

- Node.js 20+ (disarankan)

## Menjalankan test

```bash
node --test web/admin/app.test.js
```

## Cakupan test saat ini

- Query serializer
- Export URL builder
- State store behavior
- Date formatting
- Debounce behavior

## Catatan

- Test fokus ke utility/state logic yang paling kritikal untuk maintainability.
- Integration test DOM penuh bisa ditambah berikutnya jika diperlukan (mis. jsdom atau playwright).

# Typed secrets

This example loads secrets from environment variables or sibling file-path fields using `secret:"true"` and `secretfor`:

- `Endpoint` is a `*url.URL`, validated as an absolute HTTPS URL.
- Optional `Headers` is a custom type implementing `encoding.TextUnmarshaler` and `encoding.TextMarshaler`, with no type registration needed. It parses comma-separated curl-style headers such as `Authorization: Bearer token, X-Api-Key: key`.

Run from the repository root:

```sh
API_URL='https://example.com/api' \
API_HEADERS='Authorization: Bearer token, X-Api-Key: key' \
go run ./internal/example_secret
```

Or supply files:

```sh
printf '%s' 'https://example.com/api' > endpoint.txt
printf '%s' 'Authorization: Bearer token, X-Api-Key: key' > headers.txt
go run ./internal/example_secret --endpoint-file endpoint.txt --headers-file headers.txt
```

File paths can also come from `API_URL_FILE`, `API_HEADERS_FILE`, or JSON config supplied through `--config-file`. Direct secret values are excluded from flags, config input, and source-aware dumps. Supplying both a direct value and its file companion causes an error.

Header names are case-insensitive; repeated names retain multiple values. Each comma separates a header, so commas inside values are unsupported. Use `http.Header(p.Headers)` in application code. URL files must omit trailing newlines. The example prints only the number of header names, never secret values, and makes no network requests.

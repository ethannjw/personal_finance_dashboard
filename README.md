# Ledger

A local finance dashboard backed by Google Sheets. The backend is Go with Chi; the frontend uses plain JavaScript, HTML, CSS, and a locally bundled Tabulator 6.5.0 transaction grid. Google Sheets is the database.

## Requirements

- Go 1.22 or newer to compile the server.
- Node.js and npm to bundle frontend dependencies or run frontend tests. Node.js 20 or newer is a suitable baseline.
- Make for the provided build/test commands (or run the commands in `Makefile` directly).
- A browser, internet access to Google Sheets and Google OAuth endpoints, and a workbook matching [the workbook contract](docs/workbook.md).
- An authorized-user OAuth JSON token with a refresh token and permission to edit that workbook. The Go app refreshes access tokens but does not provide a first-time login flow.

After building, the `ledger` executable needs only its configuration, OAuth token file, and access to Google. Frontend files are embedded in the executable; Node.js and Python are not runtime requirements.

## Configure and run

From this app directory:

```bash
cp .env.example .env
chmod 600 .env
```

Edit `.env` to supply your workbook ID and OAuth token-file path, then:

```bash
make build
./ledger
```

Open <http://127.0.0.1:8765>. Press `Ctrl+C` to stop. Rebuild and restart after changing Go or frontend source because frontend assets are embedded at compile time.

The surrounding Finance workspace also provides `../run_finance_app.sh`, which explicitly loads this directory's `.env`. That wrapper is optional for a standalone clone.

## Docker

The multi-stage [Dockerfile](Dockerfile) bundles Tabulator and runs the frontend/Go checks, then compiles a static Go executable. The final image uses Alpine Linux 3.24.2 and contains the executable and HTTPS CA certificates. Go, Node.js, build dependencies, and credentials stay out of the runtime image. Official image versions and digests are pinned in the Dockerfile; update them deliberately when adopting upstream patches.

From the `finance_app` directory:

```bash
docker build -t ledger-finance:local .
```

Configure `.env` as described above, then run on Linux:

```bash
docker run --rm --name ledger-finance \
  --user "$(id -u):$(id -g)" \
  --read-only \
  -p 127.0.0.1:8765:8765 \
  --mount "type=bind,src=$PWD/.env,dst=/app/.env,readonly" \
  --mount "type=bind,src=$HOME/.config/ledger/google_token.json,dst=/run/secrets/google_token.json,readonly" \
  ledger-finance:local
```

Replace the token mount's `src` with the absolute path of your existing OAuth token file. Both mounted files must exist. The `--user` option uses your Linux UID/GID so private files with mode `600` remain readable without broadening their permissions; the image otherwise defaults to unprivileged UID/GID `65532`. The app does not need a writable filesystem.

The image sets `FINANCE_HOST=0.0.0.0` so Docker can forward traffic, and `GOOGLE_TOKEN_FILE=/run/secrets/google_token.json` so it reads the mounted token. These environment defaults override their values in `.env`. Publishing to `127.0.0.1` keeps the app accessible locally. Open <http://127.0.0.1:8765> and stop with `Ctrl+C`.

Mount `.env` rather than passing it as Docker's `--env-file`: the app's parser handles the quoted tab titles in `.env.example`. Alternatively, omit the configuration mount and supply individual settings with `-e FINANCE_SHEET_ID=your-workbook-id` and other `-e` options. Credentials belong in runtime secret files or read-only mounts, never build arguments or image layers.

The `.dockerignore` uses an allowlist of build inputs, so local configuration and private data are not sent to the builder. The image health check uses `/api/health` and checks process availability, not Google connectivity. The app still requires authentication/access controls before public exposure.

## Configuration

See [.env.example](.env.example) for a shareable configuration template.

- `FINANCE_SHEET_ID`: required. The ID between `/d/` and `/edit` in a Google Sheets URL; do not paste the complete URL.
- `GOOGLE_TOKEN_FILE`: required. Path to an authorized-user OAuth JSON file. Keep credentials outside the repository. `~/` is expanded; relative paths resolve from the directory containing the selected `.env` file.
- `FINANCE_TRANSACTIONS_TAB`: transaction tab title; defaults to `📋 Transactions`.
- `FINANCE_CATEGORIES_TAB`: category tab title; defaults to `🏷️ Categories`.
- `FINANCE_HOST`: defaults to `127.0.0.1`.
- `FINANCE_PORT`: defaults to `8765`; must be between 1 and 65535.

The Go executable looks for `.env` in its working directory, then beside the executable. Choose another file with `./ledger --env /path/to/.env`. A missing file is allowed if the required settings are supplied through environment variables. Existing environment variables override file values, including explicitly empty variables. CLI `--host` and `--port` override those respective settings:

```bash
./ledger --env .env --port 9000
```

Files support literal `KEY=value` entries, optional matching single/double quotes, blank lines, and full-line `#` comments. They do not support inline comments, `export`, shell commands, variable interpolation, or escape expansion. Never `source` the file to run the app.

Configuration is read at startup. Changes to `.env` require restarting, but not rebuilding.

## Google authorization

Enable the Google Sheets API in your Google Cloud project and authorize a Google account that has edit access to the workbook. Google's [Sheets Python quickstart](https://developers.google.com/workspace/sheets/api/quickstart/python) describes a one-time authorization flow that produces an authorized-user `token.json`. For this app's editing features, use the `https://www.googleapis.com/auth/spreadsheets` scope instead of the quickstart's read-only scope; see [Google's scope documentation](https://developers.google.com/workspace/sheets/api/scopes). If an existing grant only has read access, authorize again with the write scope.

Point `GOOGLE_TOKEN_FILE` at the resulting token file, not the downloaded OAuth client-registration `credentials.json` or a service-account key. The file must contain `refresh_token`, `client_id`, `client_secret`, and `token_uri`. It can also contain `token`, `expiry`, `token_type`, and `scopes`. The account represented by that file must be able to read and edit the configured workbook. Refresh happens in memory; the Go server does not rewrite the token file.

The server has no application login or multi-user permission system. Its default loopback address is intended for local use. Binding to a public interface requires separate authentication and deployment work.

## App structure

- `main.go`: startup, configuration flags, embedded frontend files, HTTP timeouts, graceful shutdown.
- `config.go`: `.env` parsing, environment overrides, required-setting validation, path resolution.
- `server.go`: Chi routes, JSON request/response handling, validation of request bodies, static-file serving.
- `repository.go`: the `FinanceRepository` interface and Google Sheets implementation; OAuth, workbook schema validation, a 20-second data cache, ID-based updates, and batch category writes.
- `static/index.html`: layout and controls for Overview and Transactions.
- `static/app.js`: browser navigation, separate filter state for each view, report calculations, SVG charts, transaction-grid setup, edits, and CSV export.
- `static/styles.css`: layout and styling.
- `static/vendor/`: bundled Tabulator assets; `package.json` and `package-lock.json` pin their source dependency.
- `*_test.go` and `tests/frontend.test.cjs`: Go and frontend regression tests.
- `server.py`, `__init__.py`, and `tests/test_server.py`: legacy Python implementation and its tests. These are not used by the Go executable.

All Go files use one `main` package. HTTP handlers depend on the repository interface so tests can replace Google Sheets with a fake. Overview and Transactions are browser views inside one HTML page, not separate server-rendered routes.

On load, the browser requests `/api/data`. The handler asks the repository for workbook rows, and returns JSON. The browser applies filters and computes charts locally. Edits go through the JSON API and update Google Sheets directly.

### API

- `GET /api/health`: process health only; does not verify Google connectivity.
- `GET /api/data`: transactions, categories, workbook link, and sync time. Add `?refresh=1` to bypass the cache.
- `PATCH /api/transactions/{transactionID}`: category, description, notes, or tracked status for one entry.
- `PATCH /api/transactions/categories`: bulk category update using `{ "ids": ["transaction-id"], "category": "Other" }`.
- Frontend assets are served from the same origin. No CDN is required at runtime.

## Views and edits

Overview and Transactions remember separate bucket, year, month, category, status, and search filters while switching pages. Overview metrics and charts use only Overview filters. Transaction column filters and CSV export belong to Transactions.

The grid supports sorting, column filters, pagination, movable/resizable columns, inline editing, and double-click editing. Amount filters accept expressions such as `50`, `>100`, `<=20`, and `10..100`. Grid settings persist in browser storage; top-level page filters last for the current browser session and reset on reload.

Checkboxes select rows for bulk changes. The header checkbox selects the current page; selections carry across table pages and clear when table data is replaced. Bulk categories use one Google Sheets batch request after validating every selected ID. Other bulk fields save one at a time. Failed selections remain selected; after a network failure, refresh to verify whether Google applied the write before retrying.

Every write resolves a stable transaction ID against column A, rather than trusting browser row numbers. Writes affect only Category (G), Description (I), and Notes (O). Tracked status changes the category to `Internal Transfer`, or restores an included Auto Category / `Other`; it does not overwrite the inclusion formula in column L.

## Tests

```bash
make test
```

This runs Go tests, `go vet`, JavaScript syntax validation, and frontend regression tests. Tests use fake Google responses and do not contact Google or modify a workbook. To run the legacy Python tests, use an existing Python environment with pytest:

```bash
python3 -m pytest -q tests/test_server.py
```

## Publishing the source

Publish this `finance_app` directory as the repository root. It works independently of the surrounding bank-statement organiser and spending-tracker scripts. Commit `.env.example`, source, documentation, Go/npm manifests and lockfiles, tests, and bundled vendor assets.

The included `.gitignore` excludes `.env` files (except `.env.example`), the compiled executable, `node_modules`, common credential filenames, logs, and PDF/CSV/XLSX exports. Ignore rules do not remove already tracked files or clean old commits. Before publishing, review staged files and any existing Git history for private data; keep token files outside the repository regardless of filename.

Do not publish the surrounding Finance directory: its bank statements, manifests, tracker datasets, and personal configuration are outside this app's shareable source. The app does not ingest statements or create a workbook automatically; provide compatible rows using your own import workflow.

## Legacy Python server

The legacy server reads this directory's `.env` and the same workbook/tab/host/port environment settings. It additionally requires `GOOGLE_HELPER_DIR` to point to an existing Google Workspace helper containing `google_api.py`, plus that helper's Google Python dependencies and authentication setup. It does not use `GOOGLE_TOKEN_FILE` directly and has no Go `--env` flag or bulk-category endpoint. Use the Go server for standalone operation.

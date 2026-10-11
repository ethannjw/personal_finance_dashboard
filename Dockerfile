# Build with the finance_app directory as the context.
FROM node:24.21.0-alpine3.24@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS frontend
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY static/app.js ./static/app.js
COPY tests/frontend.test.cjs ./tests/frontend.test.cjs
RUN npm run build && node --check static/app.js && node tests/frontend.test.cjs

FROM golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY static ./static
COPY --from=frontend /src/static/vendor ./static/vendor
RUN go test ./... && go vet ./... && \
    go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/ledger .

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS runtime
WORKDIR /app
# Google OAuth and Sheets use HTTPS; include trusted roots without adding tooling.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/ledger /usr/local/bin/ledger
ENV HOME=/app \
    FINANCE_HOST=0.0.0.0 \
    FINANCE_PORT=8765 \
    GOOGLE_TOKEN_FILE=/run/secrets/google_token.json
USER 65532:65532
EXPOSE 8765
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${FINANCE_PORT}/api/health" || exit 1
ENTRYPOINT ["/usr/local/bin/ledger"]

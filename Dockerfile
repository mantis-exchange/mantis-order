# ---------- builder ----------
FROM golang:1.25-bookworm AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o /mantis-order ./cmd/order

# ---------- runtime ----------
FROM gcr.io/distroless/static-debian12

COPY --from=builder /mantis-order /mantis-order

EXPOSE 50052

CMD ["/mantis-order"]

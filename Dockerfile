FROM golang:1.24.13 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/k8s-mcp ./

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/k8s-mcp /app/k8s-mcp
COPY kb /app/kb
ENV PORT=8080
ENV KB_DIR=/app/kb
EXPOSE 8080
ENTRYPOINT ["/app/k8s-mcp"]

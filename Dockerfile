FROM golang:1.26.11-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tribunpass ./cmd/tribunpass

FROM alpine:3.23
COPY --from=build /tribunpass /usr/local/bin/tribunpass
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["tribunpass"]

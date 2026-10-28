FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services ./services
RUN CGO_ENABLED=0 go build -o /out/orders ./services/orders && \
    CGO_ENABLED=0 go build -o /out/inventory ./services/inventory && \
    CGO_ENABLED=0 go build -o /out/payments ./services/payments && \
    CGO_ENABLED=0 go build -o /out/notifications ./services/notifications

FROM alpine:3.20
RUN adduser -D -H app
WORKDIR /app
COPY --from=build /out/ /app/
USER app
EXPOSE 8080 8081 8082 8083
CMD ["/app/orders"]

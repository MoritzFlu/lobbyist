FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /lobbyist .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /lobbyist /usr/local/bin/
EXPOSE 8080
VOLUME /data
ENV LOBBYIST_DATA_DIR=/data
ENTRYPOINT ["lobbyist"]

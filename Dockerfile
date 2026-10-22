# syntax=docker/dockerfile:1

FROM node:24-alpine AS dashboard
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=dashboard /src/web/build web/build
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kioku-sync .

FROM alpine:3.22
COPY --from=build /out/kioku-sync /usr/local/bin/kioku-sync
VOLUME /data
ENV KIOKU_SYNC_DATA=/data
EXPOSE 8756
ENTRYPOINT ["kioku-sync"]
CMD ["serve"]

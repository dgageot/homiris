ARG BUILD_FROM=ghcr.io/home-assistant/amd64-base:3.21

FROM --platform=${BUILDPLATFORM} golang:1.26-alpine AS build
ARG TARGETARCH
ARG TARGETVARIANT
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o /out/homiris .

FROM ${BUILD_FROM}
COPY --from=build /out/homiris /usr/bin/homiris
COPY --chmod=755 run.sh /run.sh
CMD ["/run.sh"]

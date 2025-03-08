FROM alpine:3.21.3 AS base

RUN echo 'https://dl-cdn.alpinelinux.org/alpine/edge/testing' >> /etc/apk/repositories

RUN apk update && \
    apk add --no-cache \
    # base
    bash bash-completion vim jq \
    # network
    bind-tools iputils curl nmap net-tools mtr netcat-openbsd bridge-utils iperf \
    # certificates
    ca-certificates openssl \
    # processes/io
    lsof htop atop strace sysstat ltrace ncdu hdparm pciutils psmisc tree pv \
    # kubernetes
    kubectl

# Non-root target
FROM base AS nonroot
RUN addgroup -S -g 1000 nonroot && \
    adduser -S -u 1000 -G nonroot nonroot && \
    chown -R nonroot:nonroot /home/nonroot
USER 1000:1000

# Root target
FROM base AS root
USER 0:0

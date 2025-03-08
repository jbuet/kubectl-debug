FROM alpine:3.21.3

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

ARG ROOT_USER=false
RUN if [ "$ROOT_USER" = "false" ]; then \
        addgroup -S -g 1000 nonroot && \
        adduser -S -u 1000 -G nonroot nonroot; \
    fi

# Set proper permissions for non-root user
RUN if [ "$ROOT_USER" = "false" ]; then \
        chown -R nonroot:nonroot /home/nonroot; \
    fi

USER ${ROOT_USER:+0:0}${ROOT_USER:-1000:1000}

ENTRYPOINT ["bash"]

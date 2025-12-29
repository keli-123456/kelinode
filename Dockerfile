# Build go
FROM golang:1.25.0-alpine AS builder
WORKDIR /app
COPY . .
ENV CGO_ENABLED=0
ENV GOFLAGS=-modcacherw
RUN GOEXPERIMENT=jsonv2 go mod download
RUN set -eux; \
    modcache="$(GOEXPERIMENT=jsonv2 go env GOMODCACHE)"; \
    freedom_go="$(find "$modcache" -path '*/github.com/wyx2685/xray-core@*/proxy/freedom/freedom.go' -print -quit || true)"; \
    if [ -z "$freedom_go" ]; then \
      freedom_go="$(find "$modcache" -path '*/github.com/xtls/xray-core@*/proxy/freedom/freedom.go' -print -quit || true)"; \
    fi; \
    if [ -z "$freedom_go" ] || [ ! -f "$freedom_go" ]; then \
      echo "v2node: freedom.go not found in module cache" >&2; \
      exit 1; \
    fi; \
    if grep -q 'b.UDP.Address.Family().IsDomain()' "$freedom_go" && ! grep -q 'b.UDP.Address == nil' "$freedom_go"; then \
      chmod u+w "$freedom_go" || true; \
      tmp="$(mktemp)"; \
      awk '\
        /if b\\.UDP\\.Address\\.Family\\(\\)\\.IsDomain\\(\\) \\{/ && !patched {\
          print "\t\t\tif b.UDP.Address == nil {";\
          print "\t\t\t\tb.Release()";\
          print "\t\t\t\tcontinue";\
          print "\t\t\t}";\
          patched=1;\
        }\
        { print }\
      ' "$freedom_go" > "$tmp"; \
      cat "$tmp" > "$freedom_go"; \
      rm "$tmp"; \
    fi; \
    true
RUN GOEXPERIMENT=jsonv2 go build -v -o v2node

# Release
FROM  alpine
# 安装必要的工具包
RUN  apk --update --no-cache add tzdata ca-certificates curl jq \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
RUN mkdir /etc/v2node/
COPY --from=builder /app/v2node /usr/local/bin

COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["v2node", "server"]

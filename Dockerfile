FROM golang:1.21 as builder
WORKDIR /workdir
COPY . .
RUN make build

FROM gcr.io/distroless/static-debian11:nonroot

USER 20000:20000
COPY --from=builder --chmod=555 /workdir/external-dns-gcore-webhook /external-dns-gcore-webhook

ENTRYPOINT ["/external-dns-gcore-webhook"]
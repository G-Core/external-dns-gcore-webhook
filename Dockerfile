FROM gcr.io/distroless/static-debian11:nonroot

USER 20000:20000
ADD --chmod=555 external-dns-gcore-webhook /external-dns-gcore-webhook

ENTRYPOINT ["/external-dns-gcore-webhook"]
FROM dxflrs/garage:v2.5.0 AS garage

FROM alpine:3.22
COPY --from=garage /garage /usr/local/bin/garage
COPY garage.toml /etc/garage.toml
COPY entrypoint.sh /entrypoint.sh
EXPOSE 3900
ENTRYPOINT ["/entrypoint.sh"]

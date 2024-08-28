FROM debian
EXPOSE 8086/tcp
ENV DB_HOST="localhost" \
    DB_PORT="5432" \
    DB_USER="mypguser" \
    DB_PASS="mypgpass" \
    DB_DATABASE="mypguser"
COPY ./microp /opt/microp
WORKDIR /opt
CMD [ "/opt/microp" ]

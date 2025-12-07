FROM nginx:alpine

COPY nginx.conf /etc/nginx/nginx.conf

RUN mkdir -p /etc/nginx/ssl

# openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
#   -keyout server.key -out server.crt \
#   -subj "/C=US/ST=State/L=City/O=Company/CN=localhost"
COPY ssl/server.crt /etc/nginx/ssl/
COPY ssl/server.key /etc/nginx/ssl/

EXPOSE 80 443

CMD ["nginx", "-g", "daemon off;"]
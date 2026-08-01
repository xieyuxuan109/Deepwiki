FROM node:24.13 AS builder

WORKDIR /app
COPY frontend/package*.json ./frontend/

WORKDIR /app/frontend
RUN npm install

COPY frontend/ ./

RUN npm run build


FROM nginx:1.30

COPY --from=builder /app/frontend/dist /usr/share/nginx/html

COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
FROM node:22-alpine AS frontend-builder

WORKDIR /frontend

COPY package.json package-lock.json* ./
RUN npm install

COPY . .
RUN npm run build

# 最终产物阶段
FROM alpine:3.22 AS final
COPY --from=frontend-builder /web/static /output

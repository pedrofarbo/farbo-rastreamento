# ---- build ----
FROM node:22-alpine AS build

# Não usar /app: o Vite lê a entrada /app/index.html como URL da raiz e a
# resolve para /app/app/index.html (o app do cliente), e a landing some do build.
WORKDIR /build

COPY package.json package-lock.json ./
# O cache de downloads do npm fica num cache do BuildKit, fora da camada.
RUN --mount=type=cache,target=/root/.npm npm ci --prefer-offline --no-audit --no-fund

COPY . .
ARG VITE_API_URL=
ARG VITE_WS_URL=
ARG VITE_CUSTOMER_APP_URL=/app/
ARG VITE_PANEL_URL=/login
ENV VITE_API_URL=$VITE_API_URL
ENV VITE_WS_URL=$VITE_WS_URL
ENV VITE_CUSTOMER_APP_URL=$VITE_CUSTOMER_APP_URL
ENV VITE_PANEL_URL=$VITE_PANEL_URL
RUN npm run build

# ---- runtime ----
FROM nginx:1.30-alpine

COPY --from=build /build/dist/ /usr/share/nginx/html/
RUN test -s /usr/share/nginx/html/landing.html \
    && ! grep -q 'Welcome to nginx!' /usr/share/nginx/html/landing.html \
    && rm -f /usr/share/nginx/html/index.html /usr/share/nginx/html/50x.html
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY security-headers.conf /etc/nginx/security-headers.conf

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1

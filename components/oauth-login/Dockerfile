FROM node:22.23.4-bookworm-slim
ARG SOURCE_COMMIT=unknown
LABEL org.opencontainers.image.revision=$SOURCE_COMMIT
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-venv ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts
COPY requirements.lock ./
RUN python3 -m venv /opt/login-venv && /opt/login-venv/bin/pip install --no-cache-dir -r requirements.lock
ENV PATH=/opt/login-venv/bin:$PATH NODE_ENV=production
COPY src ./src
COPY licenses ./licenses
COPY NOTICE.zh-CN.md ./
USER node
EXPOSE 18417
CMD ["node", "src/server.mjs"]

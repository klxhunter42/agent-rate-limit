FROM node:20-alpine
RUN apk add --no-cache curl
WORKDIR /app
COPY package.json ./
RUN npm install --omit=dev
COPY . .
USER 1000
EXPOSE 8081
CMD ["node", "index.js"]

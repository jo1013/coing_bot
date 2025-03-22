# 빌드 스테이지
FROM golang:1.20 as builder

WORKDIR /build

# 의존성 파일 복사 및 다운로드
COPY go.mod go.sum ./
RUN go mod download

# 소스 코드 복사
COPY . .

# 실행 파일 빌드
RUN CGO_ENABLED=0 GOOS=linux go build -o trading-bot .

# 실행 스테이지
FROM alpine:latest

WORKDIR /app

# 로그 디렉토리 생성
RUN mkdir -p /app/logs && chmod 755 /app/logs

# 빌더에서 실행 파일 복사
COPY --from=builder /build/trading-bot .

# 실행 파일 권한 설정
RUN chmod +x /app/trading-bot

EXPOSE 8888

# 실행
CMD ["/app/trading-bot"]
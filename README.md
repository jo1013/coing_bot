# 암호화폐 자동 거래 봇

업비트 API를 활용한 기술적 분석 기반 암호화폐 자동 거래 시스템입니다.

## 프로젝트 구조

```
.
├── Dockerfile             # Docker 이미지 설정
├── README.md              # 프로젝트 문서
├── docker-compose.yml     # Docker Compose 설정
├── go.mod                 # Go 모듈 정의
├── go.sum                 # Go 의존성
├── logs                   # 로그 디렉토리
└── main.go                # 메인 애플리케이션 코드
```

## 주요 기능

### 기술적 분석
- 이동평균(MA): 단기/장기 가격 추세 파악
- RSI(Relative Strength Index): 과매수/과매도 상태 분석
- 볼린저 밴드: 가격 변동성 및 이상치 감지

### 거래 전략
- 매수 조건: 단기 이동평균 > 장기 이동평균 & RSI < 30 & 현재가격 < 볼린저 하단밴드
- 매도 조건: 단기 이동평균 < 장기 이동평균 & RSI > 70 & 현재가격 > 볼린저 상단밴드
- 신뢰도 기반 포지션 사이즈 조정

### 리스크 관리
- 최대 포지션 크기 제한
- 손절/익절 수준 설정
- 계정 잔고의 2% 이내 리스크 관리

## 설치 및 실행

### 요구 사항
- Docker 및 Docker Compose
- 업비트 API 키

### 환경 변수 설정
`.env` 파일을 생성하고 다음 변수를 설정하세요:

```
UPBIT_OPEN_API_ACCESS_KEY=your_access_key
UPBIT_OPEN_API_SECRET_KEY=your_secret_key
UPBIT_OPEN_API_SERVER_URL=https://api.upbit.com
TRADING_MARKET=KRW-BTC
PORT=8080
GIN_MODE=debug
```

### Docker로 실행

```bash
# 빌드 및 실행
docker-compose up --build

# 백그라운드 실행
docker-compose up -d --build
```

## API 사용 방법

### 트레이딩 제어

```bash
# 트레이딩 시작
curl -X POST http://localhost:8080/api/start

# 상태 확인
curl http://localhost:8080/api/status

# 트레이딩 중지
curl -X POST http://localhost:8080/api/stop
```

## 주요 컴포넌트

### TradingBot
거래 봇의 핵심 구조체로 설정, 기술지표, 거래전략, 리스크 관리 등을 통합 관리합니다.

### TechnicalIndicators
가격 및 거래량 데이터를 저장하고 기술적 분석 지표를 계산하는 기능을 제공합니다.

### TradingStrategy
거래 전략 파라미터를 정의하고 매수/매도 신호를 생성합니다.

### RiskManager
거래 위험을 관리하고 적절한 포지션 크기를 계산합니다.

## 커스터마이징

### 거래 전략 수정
`main.go` 파일에서 `TradingStrategy` 구조체의 파라미터를 조정할 수 있습니다:

```go
strategy: &TradingStrategy{
    ShortMA:   10,  // 단기 이동평균 기간
    LongMA:    20,  // 장기 이동평균 기간
    RSIPeriod: 14,  // RSI 계산 기간
    BBPeriod:  20,  // 볼린저 밴드 기간
    BBStdDev:  2.0, // 볼린저 밴드 표준편차
},
```

### 리스크 관리 설정
`main.go` 파일에서 `RiskManager` 구조체의 파라미터를 조정할 수 있습니다:

```go
riskManager: &RiskManager{
    MaxPositionSize: 1000.0, // 최대 포지션 크기
    StopLoss:        2.0,    // 손절 비율(%)
    TakeProfit:      3.0,    // 익절 비율(%)
},
```

## 주의사항

- 이 코드는 참고용으로만 사용하세요. 실제 거래에 사용하기 전에 충분한 테스트가 필요합니다.
- 암호화폐 시장은 변동성이 높아 손실이 발생할 수 있습니다.
- API 키는 안전하게 관리하고, 필요한 권한만 부여하세요.

## 라이센스
MIT
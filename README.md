# 암호화폐 업비트 자동 거래 봇

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
- **이동평균(MA)**: 단기/장기 가격 추세 파악
  - 특정 기간 동안의 평균 가격을 계산하여 추세를 부드럽게 보여줌
  - 단기 이동평균이 장기 이동평균을 상회하면 상승 추세, 하회하면 하락 추세로 해석
- **RSI(Relative Strength Index)**: 과매수/과매도 상태 분석
  - 0~100 사이 값으로 표현되는 모멘텀 지표
  - 70 이상: 과매수 상태로 가격 하락 가능성
  - 30 이하: 과매도 상태로 가격 상승 가능성
- **볼린저 밴드**: 가격 변동성 및 이상치 감지
  - 중간선: 이동평균
  - 상단선과 하단선: 중간선에서 표준편차의 배수만큼 떨어진 선
  - 가격이 상단선을 넘으면 고평가, 하단선을 밑돌면 저평가 가능성

### 거래 전략 (반전 전략)
- **매수 조건**: 다음 조건이 모두 충족될 때 매수 신호 생성
  - 단기 이동평균 > 장기 이동평균 (상승 추세)
  - RSI < 30 (과매도 상태)
  - 현재가격 < 볼린저 하단밴드 (비정상적으로 낮은 가격)
- **매도 조건**: 다음 조건이 모두 충족될 때 매도 신호 생성
  - 단기 이동평균 < 장기 이동평균 (하락 추세)
  - RSI > 70 (과매수 상태)
  - 현재가격 > 볼린저 상단밴드 (비정상적으로 높은 가격)
- **신뢰도 계산**: 각 지표의 신호 강도에 가중치를 부여
  - 이동평균 차이: 40%
  - RSI 강도: 30%
  - 볼린저 밴드 이탈 정도: 30%
  - 0~1 사이의 값으로 정규화하여 포지션 크기 결정에 활용

### 리스크 관리
- **포지션 크기 제한**
  - 기본적으로 계좌 잔액의 2%만 사용
  - 신호 신뢰도에 따라 포지션 크기 조정
  - 설정된 최대 포지션 크기(MaxPositionSize)로 제한
- **손절/익절 수준 설정**
  - 손절(StopLoss): 포지션에서 설정된 비율(예: 2%) 이상 손실 발생 시 청산 고려
  - 익절(TakeProfit): 포지션에서 설정된 비율(예: 3%) 이상 이익 발생 시 청산 고려
- **리스크 분산**
  - 스탑로스 기반으로 포지션 크기 추가 제한 (총 리스크가 2%를 넘지 않도록)
  - 일일 최대 거래 금액(DailyLimit) 설정으로 과도한 거래 방지

## 거래 프로세스 흐름

1. **가격 데이터 수집**
   - 정의된 시장(예: KRW-BTC)의 현재 가격 조회
   - 최대 100개의 최근 가격 데이터 유지

2. **기술적 분석**
   - 이동평균(MA), RSI, 볼린저 밴드 등의 지표 계산
   - 매수/매도 신호 및 신뢰도 분석

3. **거래 결정**
   - "hold" 신호인 경우 아무 조치 없음
   - "buy" 또는 "sell" 신호일 경우 계좌 잔고 확인 및 포지션 크기 계산

4. **주문 실행**
   - 업비트 API를 통해 주문 실행 (지정가 주문)
   - 주문 결과 로깅 및 모니터링

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

### 인증 토큰 발급
```bash
# JWT 토큰 발급
curl -X POST http://localhost:8080/token -H "Content-Type: application/json" -d '{}'
```

### 트레이딩 제어
```bash
# 트레이딩 시작 (토큰 인증 필요)
curl -X POST http://localhost:8080/api/start -H "Authorization: Bearer YOUR_TOKEN"

# 상태 확인 (토큰 인증 필요)
curl http://localhost:8080/api/status -H "Authorization: Bearer YOUR_TOKEN"

# 트레이딩 중지 (토큰 인증 필요)
curl -X POST http://localhost:8080/api/stop -H "Authorization: Bearer YOUR_TOKEN"
```

## 주요 컴포넌트 상세 설명

### TradingBot
거래 봇의 핵심 구조체로 다음 요소를 통합 관리합니다:
- **Config**: API 키 및 서버 설정
- **TechnicalIndicators**: 기술적 분석 지표 계산 및 가격 데이터 관리
- **TradingStrategy**: 거래 전략 및 신호 생성
- **RiskManager**: 리스크 관리 및 포지션 크기 계산
- **Logger**: 로그 기록 기능

### TechnicalIndicators
가격 및 거래량 데이터를 저장하고 다음 기술적 분석 기능을 제공합니다:
- **calculateMA()**: 이동평균 계산
- **calculateRSI()**: RSI 지표 계산
- **calculateBollingerBands()**: 볼린저 밴드 계산

### TradingStrategy
거래 전략 파라미터와 신호 생성 로직을 포함합니다:
- **analyzeSignals()**: 여러 지표를 결합하여 매수/매도/홀드 신호 생성
- **calculateConfidence()**: 신호의 신뢰도 계산

### RiskManager
리스크 관리 메커니즘을 제공합니다:
- **calculatePositionSize()**: 신호의 신뢰도와 계좌 잔고를 고려한 적정 포지션 크기 계산
- **checkRisk()**: 현재 포지션의 리스크 수준 평가

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
    MaxDrawdown:     5.0,    // 최대 손실 허용 비율(%)
    DailyLimit:      10000.0, // 일일 최대 거래 금액
},
```

### 소스 코드 구조 및 주요 함수
주요 함수와 역할:
- **executeTradeLoop()**: 거래 실행 주기 (가격 조회 → 분석 → 주문)
- **analyzeSignals()**: 기술적 지표를 기반으로 거래 신호 생성
- **calculatePositionSize()**: 리스크 관리 기반 포지션 크기 계산
- **executeTrade()**: API를 통한 실제 주문 실행
- **fetchCurrentPrice()**: 현재 가격 조회
- **getBalance()**: 계좌 잔고 조회

## 트레이딩 전략 특성

이 봇은 주로 **과도한 가격 움직임의 반전**을 노리는 전략(Reversal Strategy)을 사용합니다:

- **반전 매수**: 가격이 비정상적으로 낮아졌을 때(과매도) 매수하여 반등을 노림
- **반전 매도**: 가격이 비정상적으로 높아졌을 때(과매수) 매도하여 하락을 노림

이러한 전략은 다음과 같은 특성을 가집니다:
- **강점**: 극단적인 시장 상황에서 높은 수익 가능성
- **약점**: 지속적인 추세 상황에서 반복적인 손실 가능성
- **적합한 시장**: 높은 변동성과 레인지 바운드 시장(일정 범위 내 등락)

## 주의사항

- 이 코드는 참고용으로만 사용하세요. 실제 거래에 사용하기 전에 충분한 테스트가 필요합니다.
- 암호화폐 시장은 변동성이 높아 손실이 발생할 수 있습니다.
- API 키는 안전하게 관리하고, 필요한 권한만 부여하세요.
- 백테스팅(과거 데이터 테스트)을 통해 전략의 성능을 검증한 후 사용하세요.
- 트레이딩 파라미터는 시장 상황에 맞게 주기적으로 조정하는 것이 좋습니다.


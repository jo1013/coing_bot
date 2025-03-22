package main

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv" // 이 라인 추가
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

type TechnicalIndicators struct {
	Prices []float64
	Volume []float64
}

// Logger 구조체 정의
type Logger struct {
	EnableDebug bool
	LogFile     *os.File
}

// 로깅 함수들
func (l *Logger) Info(format string, v ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logMsg := fmt.Sprintf("[INFO] %s: %s\n", timestamp, fmt.Sprintf(format, v...))
	log.Print(logMsg)
	if l.LogFile != nil {
		l.LogFile.WriteString(logMsg)
	}
}

func (l *Logger) Debug(format string, v ...interface{}) {
	if !l.EnableDebug {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logMsg := fmt.Sprintf("[DEBUG] %s: %s\n", timestamp, fmt.Sprintf(format, v...))
	log.Print(logMsg)
	if l.LogFile != nil {
		l.LogFile.WriteString(logMsg)
	}
}

func (l *Logger) Error(format string, v ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logMsg := fmt.Sprintf("[ERROR] %s: %s\n", timestamp, fmt.Sprintf(format, v...))
	log.Print(logMsg)
	if l.LogFile != nil {
		l.LogFile.WriteString(logMsg)
	}
}

// 이동평균 계산
func (t *TechnicalIndicators) calculateMA(period int) float64 {
	if len(t.Prices) < period {
		return 0
	}
	sum := 0.0
	for i := len(t.Prices) - period; i < len(t.Prices); i++ {
		sum += t.Prices[i]
	}
	return sum / float64(period)
}

// RSI 계산
func (t *TechnicalIndicators) calculateRSI(period int) float64 {
	if len(t.Prices) < period+1 {
		return 0
	}

	gains := 0.0
	losses := 0.0
	for i := len(t.Prices) - period; i < len(t.Prices); i++ {
		change := t.Prices[i] - t.Prices[i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	if losses == 0 {
		return 100
	}

	rs := gains / losses
	return 100 - (100 / (1 + rs))
}

// 볼린저 밴드 계산
func (t *TechnicalIndicators) calculateBollingerBands(period int, stdDev float64) (middle, upper, lower float64) {
	if len(t.Prices) < period {
		return 0, 0, 0
	}

	middle = t.calculateMA(period)

	variance := 0.0
	for i := len(t.Prices) - period; i < len(t.Prices); i++ {
		variance += math.Pow(t.Prices[i]-middle, 2)
	}
	sd := math.Sqrt(variance / float64(period))

	upper = middle + (sd * stdDev)
	lower = middle - (sd * stdDev)

	return middle, upper, lower
}

// Configuration 구조체
type Config struct {
	AccessKey string
	SecretKey string
	Port      string
}

// JWT 클레임 구조체
type Claims struct {
	AccessKey    string `json:"access_key"`
	Nonce        string `json:"nonce"`
	QueryHash    string `json:"query_hash,omitempty"`
	QueryHashAlg string `json:"query_hash_alg,omitempty"`
	jwt.StandardClaims
}

// 1. TradingBot 구조체에 cancelFunc 필드 추가
type TradingBot struct {
	config      Config
	indicators  *TechnicalIndicators
	strategy    *TradingStrategy
	riskManager *RiskManager
	isRunning   bool
	mu          sync.RWMutex
	logger      *Logger
	cancelFunc  context.CancelFunc
}

// 2. 트레이딩 타입 변환 함수 추가
func convertSignalTypeToUpbitSide(signalType string) string {
	switch signalType {
	case "buy":
		return "bid"
	case "sell":
		return "ask"
	default:
		return ""
	}
}

// TradeSignal 구조체
type TradeSignal struct {
	Type       string // "buy", "sell", "hold"
	Price      float64
	Volume     float64
	Confidence float64
}

// RiskManager 구조체 및 메서드
type RiskManager struct {
	MaxPositionSize float64
	StopLoss        float64
	TakeProfit      float64
	MaxDrawdown     float64
	DailyLimit      float64
}

func (rm *RiskManager) calculatePositionSize(signal TradeSignal, balance float64, currentPrice float64) float64 {
	// 1. 기본 포지션 크기 계산
	baseSize := balance * 0.02 // 기본적으로 계좌의 2% 사용

	// 2. 신뢰도에 따른 조정
	adjustedSize := baseSize * signal.Confidence

	// 3. 최대 포지션 크기 제한
	if adjustedSize > rm.MaxPositionSize {
		adjustedSize = rm.MaxPositionSize
	}

	// 4. 스탑로스 기반 포지션 크기 조정
	riskAmount := currentPrice * (rm.StopLoss / 100)
	if riskAmount > 0 {
		maxSizeByRisk := (balance * 0.02) / riskAmount // 최대 2% 리스크
		if adjustedSize > maxSizeByRisk {
			adjustedSize = maxSizeByRisk
		}
	}

	return adjustedSize
}

// 리스크 체크
func (rm *RiskManager) checkRisk(position float64, currentPrice float64, entryPrice float64) bool {
	// 스탑로스 체크
	loss := ((entryPrice - currentPrice) / entryPrice) * 100
	if loss > rm.StopLoss {
		return false
	}

	// 익절 체크
	profit := ((currentPrice - entryPrice) / entryPrice) * 100
	if profit > rm.TakeProfit {
		return false
	}

	return true
}

// fetchCurrentPrice 함수 수정 - 더 많은 오류 검사 추가
func (bot *TradingBot) fetchCurrentPrice(market string) (float64, error) {
	if market == "" {
		return 0, fmt.Errorf("market parameter is empty")
	}

	apiUrl := fmt.Sprintf("%s/v1/ticker?markets=%s",
		os.Getenv("UPBIT_OPEN_API_SERVER_URL"),
		market)

	bot.logger.Debug("Fetching price from: %s", apiUrl)

	client := &http.Client{Timeout: time.Second * 10}
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("API request failed: %v", err)
	}
	defer resp.Body.Close()

	// 응답 상태 코드 확인
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("API returned non-200 status: %d, body: %s",
			resp.StatusCode, string(bodyBytes))
	}

	// Upbit API 응답 구조체
	type UpbitTicker struct {
		TradePrice float64 `json:"trade_price"`
		Market     string  `json:"market"`
		Timestamp  int64   `json:"timestamp"`
	}

	var tickers []UpbitTicker
	if err := json.NewDecoder(resp.Body).Decode(&tickers); err != nil {
		return 0, fmt.Errorf("failed to decode response: %v", err)
	}

	if len(tickers) == 0 {
		return 0, fmt.Errorf("no price data available for market: %s", market)
	}

	// 가격이 0인지 확인
	if tickers[0].TradePrice <= 0 {
		return 0, fmt.Errorf("invalid price data (zero or negative) for market: %s", market)
	}

	return tickers[0].TradePrice, nil
}

// TradingStrategy 수정된 분석 함수
func (ts *TradingStrategy) analyzeSignals(indicators *TechnicalIndicators) TradeSignal {
	shortMA := indicators.calculateMA(ts.ShortMA)
	longMA := indicators.calculateMA(ts.LongMA)
	rsi := indicators.calculateRSI(ts.RSIPeriod)
	_, upperBB, lowerBB := indicators.calculateBollingerBands(ts.BBPeriod, ts.BBStdDev)

	currentPrice := indicators.Prices[len(indicators.Prices)-1]
	signal := TradeSignal{
		Type:  "hold",
		Price: currentPrice,
	}

	// 매수 신호
	if shortMA > longMA && rsi < 30 && currentPrice < lowerBB {
		signal.Type = "buy"
		signal.Volume = 0.0 // 실제 거래량은 RiskManager에서 계산
		signal.Confidence = calculateConfidence(shortMA, longMA, rsi, currentPrice, lowerBB)
	}

	// 매도 신호
	if shortMA < longMA && rsi > 70 && currentPrice > upperBB {
		signal.Type = "sell"
		signal.Volume = 0.0 // 실제 거래량은 RiskManager에서 계산
		signal.Confidence = calculateConfidence(shortMA, longMA, rsi, currentPrice, upperBB)
	}

	return signal
}

// 신뢰도 계산 함수 (0~1 사이 값 반환)
func calculateConfidence(shortMA, longMA, rsi, price, band float64) float64 {
	// MA 시그널 강도
	maSignal := math.Abs(shortMA-longMA) / longMA

	// RSI 시그널 강도
	rsiSignal := 0.0
	if rsi < 30 {
		rsiSignal = (30 - rsi) / 30
	} else if rsi > 70 {
		rsiSignal = (rsi - 70) / 30
	}

	// 밴드 이탈 강도
	bandSignal := math.Abs(price-band) / band

	// 종합 신뢰도 계산 (각 지표의 가중 평균)
	confidence := (maSignal*0.4 + rsiSignal*0.3 + bandSignal*0.3)

	// 0~1 사이로 정규화
	if confidence > 1 {
		confidence = 1
	}

	return confidence
}
func NewTradingBot(config Config) *TradingBot {
	// 로그 디렉토리 확인 및 생성
	if err := os.MkdirAll("/app/logs", 0755); err != nil {
		log.Printf("Warning: Failed to create log directory: %v", err)
	}
	// 로그 파일 설정
	logFile, err := os.OpenFile("/app/logs/trading.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Warning: Failed to open log file: %v. Logs will only go to stdout.", err)
	}
	logger := &Logger{
		EnableDebug: true,
		LogFile:     logFile,
	}
	// 환경 변수 검증
	market := os.Getenv("TRADING_MARKET")
	if market == "" {
		logger.Error("TRADING_MARKET environment variable is not set")
	}
	apiUrl := os.Getenv("UPBIT_OPEN_API_SERVER_URL")
	if apiUrl == "" {
		logger.Error("UPBIT_OPEN_API_SERVER_URL environment variable is not set. Using https://api.upbit.com as default.")
		os.Setenv("UPBIT_OPEN_API_SERVER_URL", "https://api.upbit.com")
	}
	return &TradingBot{
		config:     config,
		indicators: &TechnicalIndicators{},
		strategy: &TradingStrategy{
			ShortMA:   10,
			LongMA:    20,
			RSIPeriod: 14,
			BBPeriod:  20,
			BBStdDev:  2.0,
		},
		riskManager: &RiskManager{
			MaxPositionSize: 1000.0,
			StopLoss:        2.0,
			TakeProfit:      3.0,
			MaxDrawdown:     5.0,
			DailyLimit:      10000.0,
		},
		logger: logger,
	}
}

// StartTrading 함수 수정 - 컨텍스트 추가
func (bot *TradingBot) StartTrading(interval time.Duration) {
	bot.mu.Lock()
	if bot.isRunning {
		bot.logger.Info("Trading bot is already running")
		bot.mu.Unlock()
		return
	}
	bot.isRunning = true
	bot.mu.Unlock()

	bot.logger.Info("Starting trading with interval: %v", interval)

	// 컨텍스트로 취소 처리
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				bot.executeTradeLoop()
			case <-ctx.Done():
				bot.logger.Info("Trading stopped")
				return
			}
		}
	}()

	// 취소 함수를 저장하면 나중에 StopTrading에서 사용 가능
	bot.cancelFunc = cancel
}

// Market Event 구조체
type MarketEvent struct {
	Warning bool   `json:"warning"` // 유의종목 여부
	Caution string `json:"caution"` // 주의종목 타입
}

// Market 구조체
type Market struct {
	Market      string      `json:"market"`       // 마켓 코드 (예: KRW-BTC)
	KoreanName  string      `json:"korean_name"`  // 한글 이름
	EnglishName string      `json:"english_name"` // 영문 이름
	MarketEvent MarketEvent `json:"market_event"` // 시장 경고 정보
}

// 마켓 정보를 가져오는 함수
func (bot *TradingBot) fetchMarkets() ([]Market, error) {
	apiUrl := "https://api.upbit.com/v1/market/all?is_details=true"

	client := &http.Client{
		Timeout: time.Second * 10,
	}

	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %v", err)
	}
	defer resp.Body.Close()

	var markets []Market
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	// 안전한 마켓만 필터링 (옵션)
	safeMarkets := make([]Market, 0)
	for _, market := range markets {
		// 경고나 주의가 없는 마켓만 선택
		if !market.MarketEvent.Warning && market.MarketEvent.Caution == "" {
			safeMarkets = append(safeMarkets, market)
		}
	}

	return safeMarkets, nil
}

type TradingStrategy struct {
	ShortMA   int
	LongMA    int
	RSIPeriod int
	BBPeriod  int
	BBStdDev  float64
}

// 특정 마켓이 거래하기에 안전한지 확인하는 함수
func isMarketSafe(market Market) bool {
	if market.MarketEvent.Warning {
		return false
	}

	// 주의 종목 타입 체크
	cautionTypes := []string{
		"PRICE_FLUCTUATIONS",              // 가격 급등락
		"TRADING_VOLUME_SOARING",          // 거래량 급등
		"DEPOSIT_AMOUNT_SOARING",          // 입금량 급등
		"GLOBAL_PRICE_DIFFERENCES",        // 가격 차이
		"CONCENTRATION_OF_SMALL_ACCOUNTS", // 소수 계정 집중
	}

	for _, cautionType := range cautionTypes {
		if market.MarketEvent.Caution == cautionType {
			return false
		}
	}

	return true
}

// 안전한 거래 가능 마켓 목록 출력
func printSafeMarkets(markets []Market) {
	fmt.Println("=== 안전한 거래 가능 마켓 목록 ===")
	for _, market := range markets {
		if isMarketSafe(market) {
			fmt.Printf("마켓: %s\n", market.Market)
			fmt.Printf("한글명: %s\n", market.KoreanName)
			fmt.Printf("영문명: %s\n", market.EnglishName)
			fmt.Println("------------------------")
		}
	}
}

// Price 데이터를 가져오는 함수
func (bot *TradingBot) fetchPriceData() ([]float64, error) {
	// 특정 코인(예: BTC) 가격 조회
	price, err := bot.fetchCurrentPrice("KRW-SUI")
	if err != nil {
		return nil, err
	}

	bot.mu.Lock()
	bot.indicators.Prices = append(bot.indicators.Prices, price)

	// 최대 100개의 가격 데이터만 유지
	if len(bot.indicators.Prices) > 100 {
		bot.indicators.Prices = bot.indicators.Prices[1:]
	}
	bot.mu.Unlock()

	return bot.indicators.Prices, nil
}

// 4. executeTradeLoop 함수 개선 - 로깅 일관성
func (bot *TradingBot) executeTradeLoop() {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	market := os.Getenv("TRADING_MARKET")
	if market == "" {
		bot.logger.Error("TRADING_MARKET environment variable is not set")
		return
	}
	bot.logger.Info("Starting trade loop for market: %s", market)

	// 1. 현재 가격 조회
	currentPrice, err := bot.fetchCurrentPrice(market)
	if err != nil {
		bot.logger.Error("Error fetching current price: %v", err) // log.Printf 대신 bot.logger 사용
		return
	}
	bot.logger.Debug("Current price: %f", currentPrice)

	// 2. 가격 데이터 업데이트
	bot.indicators.Prices = append(bot.indicators.Prices, currentPrice)
	if len(bot.indicators.Prices) > 100 {
		bot.indicators.Prices = bot.indicators.Prices[1:]
	}

	minDataPoints := max(bot.strategy.LongMA, bot.strategy.BBPeriod) + 1
	if len(bot.indicators.Prices) < minDataPoints {
		bot.logger.Info("Not enough price data for analysis. Have %d, need %d",
			len(bot.indicators.Prices), minDataPoints)
		return
	}
	// 3. 기술적 분석 수행
	signal := bot.strategy.analyzeSignals(bot.indicators)
	bot.logger.Debug("Trade signal: %+v", signal)

	// 4. 거래 실행
	if signal.Type == "hold" {
		bot.logger.Debug("No trade signal, holding position")
		return
	}
	// 계좌 잔고 조회
	accounts, err := bot.getBalance()
	if err != nil {
		bot.logger.Error("Error fetching balance: %v", err)
		return
	}

	// 적절한 계좌 찾기
	var balance float64
	for _, account := range accounts {
		if account.Currency == "KRW" {
			balance, err = strconv.ParseFloat(account.Balance, 64)
			if err != nil {
				bot.logger.Error("Error parsing balance: %v", err)
				return
			}
			break
		}
	}

	if balance <= 0 {
		bot.logger.Error("No KRW balance available for trading")
		return
	}
	bot.logger.Debug("Available balance: %f KRW", balance)

	// 포지션 크기 계산
	volume := bot.riskManager.calculatePositionSize(signal, balance, currentPrice)
	if volume <= 0 {
		bot.logger.Debug("Calculated trade volume is too small: %f", volume)
		return
	}
	signal.Volume = volume

	// 주문 실행
	order, err := bot.executeTrade(signal, market)
	if err != nil {
		bot.logger.Error("Error executing trade: %v", err) // log.Printf 대신 bot.logger 사용
		return
	}

	bot.logger.Info("Order executed: %+v", order) // log.Printf 대신 bot.logger 사용
}

// 설정 로드 함수
func loadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found")
	}

	// loadConfig 함수에서
	config := &Config{
		AccessKey: os.Getenv("UPBIT_OPEN_API_ACCESS_KEY"), // ACCESS_KEY -> UPBIT_OPEN_API_ACCESS_KEY
		SecretKey: os.Getenv("UPBIT_OPEN_API_SECRET_KEY"), // SECRET_KEY -> UPBIT_OPEN_API_SECRET_KEY
		Port:      os.Getenv("PORT"),
	}

	if config.AccessKey == "" || config.SecretKey == "" {
		return nil, fmt.Errorf("required environment variables are not set")
	}

	if config.Port == "" {
		config.Port = "8888"
	}

	return config, nil
}

// JWT 토큰 생성 함수
func generateToken(config Config, params map[string]string) (string, error) {
	nonce := uuid.New().String()
	claims := Claims{
		AccessKey: config.AccessKey,
		Nonce:     nonce,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: time.Now().Add(time.Minute * 10).Unix(),
			IssuedAt:  time.Now().Unix(),
		},
	}

	if len(params) > 0 {
		queryHash, err := generateQueryHash(params)
		if err != nil {
			return "", err
		}
		claims.QueryHash = queryHash
		claims.QueryHashAlg = "SHA512"
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(config.SecretKey))
}

// Account 구조체
type Account struct {
	Currency            string `json:"currency"`
	Balance             string `json:"balance"`
	Locked              string `json:"locked"`
	AvgBuyPrice         string `json:"avg_buy_price"`
	AvgBuyPriceModified bool   `json:"avg_buy_price_modified"`
	UnitCurrency        string `json:"unit_currency"`
}

// Order 구조체
type Order struct {
	UUID            string `json:"uuid"`
	Side            string `json:"side"` // "ask"(매도) 또는 "bid"(매수)
	OrdType         string `json:"ord_type"`
	Price           string `json:"price"`
	State           string `json:"state"`
	Market          string `json:"market"`
	Volume          string `json:"volume"`
	RemainingVolume string `json:"remaining_volume"`
	ExecutedVolume  string `json:"executed_volume"`
}

// 잔고 조회 함수
func (bot *TradingBot) getBalance() ([]Account, error) {
	apiUrl := os.Getenv("UPBIT_OPEN_API_SERVER_URL") + "/v1/accounts"

	// Payload 생성
	payload := map[string]interface{}{
		"access_key": os.Getenv("UPBIT_OPEN_API_ACCESS_KEY"),
		"nonce":      uuid.New().String(),
	}

	// JWT 토큰 생성
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(payload))
	jwtToken, err := token.SignedString([]byte(os.Getenv("UPBIT_OPEN_API_SECRET_KEY")))
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT token: %v", err)
	}

	// HTTP 요청
	client := &http.Client{Timeout: time.Second * 10}
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var accounts []Account
	if err := json.NewDecoder(resp.Body).Decode(&accounts); err != nil {
		return nil, err
	}

	return accounts, nil
}

// 3. 주문 실행 함수 개선 - 신호 타입 변환 및 오류 처리 추가
func (bot *TradingBot) executeTrade(signal TradeSignal, market string) (*Order, error) {
	apiUrl := os.Getenv("UPBIT_OPEN_API_SERVER_URL") + "/v1/orders"

	// 신호 타입을 Upbit API에 맞게 변환
	side := convertSignalTypeToUpbitSide(signal.Type)
	if side == "" {
		return nil, fmt.Errorf("invalid trade signal type: %s", signal.Type)
	}

	// 주문 파라미터 설정
	params := map[string]string{
		"market":   market,
		"side":     side, // 변환된 타입 사용
		"volume":   fmt.Sprintf("%.8f", signal.Volume),
		"price":    fmt.Sprintf("%.2f", signal.Price),
		"ord_type": "limit", // 지정가 주문
	}

	// Query string 생성 및 해시
	values := make(url.Values)
	for key, value := range params {
		values.Add(key, value)
	}
	queryString := values.Encode()

	// SHA512 해시 생성
	hash := sha512.New()
	hash.Write([]byte(queryString))
	queryHash := hex.EncodeToString(hash.Sum(nil))

	// JWT payload 생성
	payload := map[string]interface{}{
		"access_key":     os.Getenv("UPBIT_OPEN_API_ACCESS_KEY"),
		"nonce":          uuid.New().String(),
		"query_hash":     queryHash,
		"query_hash_alg": "SHA512",
	}

	// JWT 토큰 생성
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(payload))
	jwtToken, err := token.SignedString([]byte(os.Getenv("UPBIT_OPEN_API_SECRET_KEY")))
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT token: %v", err)
	}

	// HTTP 요청
	client := &http.Client{Timeout: time.Second * 10}
	req, err := http.NewRequest("POST", apiUrl, strings.NewReader(queryString))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 응답 상태 코드 확인
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned error status: %d, body: %s",
			resp.StatusCode, string(bodyBytes))
	}

	var order Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		return nil, fmt.Errorf("failed to decode order response: %v", err)
	}

	return &order, nil
}

// 주문 취소 함수
func (bot *TradingBot) cancelOrder(tuuid string) error {
	apiUrl := os.Getenv("UPBIT_OPEN_API_SERVER_URL") + "/v1/order"

	// Query string 생성 및 해시
	params := map[string]string{"uuid": tuuid}
	values := url.Values{}
	for key, value := range params {
		values.Add(key, value)
	}
	queryString := values.Encode()

	hash := sha512.New()
	hash.Write([]byte(queryString))
	queryHash := hex.EncodeToString(hash.Sum(nil))

	// JWT payload 생성
	payload := map[string]interface{}{
		"access_key":     os.Getenv("UPBIT_OPEN_API_ACCESS_KEY"),
		"nonce":          uuid.New().String(),
		"query_hash":     queryHash,
		"query_hash_alg": "SHA512",
	}

	// JWT 토큰 생성
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(payload))
	jwtToken, err := token.SignedString([]byte(os.Getenv("UPBIT_OPEN_API_SECRET_KEY")))
	if err != nil {
		return fmt.Errorf("failed to create JWT token: %v", err)
	}

	// HTTP 요청
	client := &http.Client{Timeout: time.Second * 10}
	req, err := http.NewRequest("DELETE", apiUrl, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	q := req.URL.Query()
	q.Add("uuid", tuuid)
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to cancel order: %s", resp.Status)
	}

	return nil
}

// 6. API 라우터 수정 - StopTrading 함수 사용
func setupRouter(bot *TradingBot) *gin.Engine {
	r := gin.Default()

	// 토큰 생성 엔드포인트
	r.POST("/token", func(c *gin.Context) {
		var params map[string]string
		if err := c.BindJSON(&params); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}

		token, err := generateToken(bot.config, params)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"token": token})
	})

	// 트레이딩 봇 제어 API
	protected := r.Group("/api")
	protected.Use(authMiddleware(bot.config))
	{
		// 트레이딩 시작
		protected.POST("/start", func(c *gin.Context) {
			go bot.StartTrading(time.Second * 30) // 30초마다 거래 체크
			c.JSON(http.StatusOK, gin.H{"message": "Trading started"})
		})

		// 트레이딩 중지 - 개선된 메서드 사용
		protected.POST("/stop", func(c *gin.Context) {
			bot.StopTrading() // 단순 플래그 설정 대신 적절한 StopTrading 함수 사용
			c.JSON(http.StatusOK, gin.H{"message": "Trading stopped"})
		})

		// 현재 상태 조회
		protected.GET("/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"is_running": bot.isRunning,
				"strategy":   bot.strategy,
			})
		})
	}

	return r
}

func main() {
	// 환경변수 로드
	config, err := loadConfig()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Gin 모드 설정
	ginMode := os.Getenv("GIN_MODE")
	if ginMode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 트레이딩 봇 초기화
	bot := NewTradingBot(*config)

	// 라우터 설정
	r := setupRouter(bot)

	// 서버 시작
	if err := r.Run(":" + config.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// 쿼리 해시 생성 함수
func generateQueryHash(params map[string]string) (string, error) {
	// 파라미터 키를 정렬
	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 정렬된 키=값 문자열 생성
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, params[k]))
	}
	queryString := strings.Join(pairs, "&")

	// SHA512 해시 생성
	hash := sha512.New()
	hash.Write([]byte(queryString))
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// 미들웨어: JWT 토큰 검증
func authMiddleware(config Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			c.Abort()
			return
		}

		// "Bearer " 제거
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		// 토큰 파싱 및 검증
		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(config.SecretKey), nil
		})

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		if claims, ok := token.Claims.(*Claims); ok && token.Valid {
			// claims를 컨텍스트에 저장
			c.Set("claims", claims)
			c.Next()
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
			c.Abort()
			return
		}
	}
}

// max 함수 추가 (Go 1.21 미만에서 필요)
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jeffail/gabs"
	"github.com/bwmarrin/discordgo"
	"github.com/go-co-op/gocron"
	"github.com/go-resty/resty/v2"
	"github.com/newrelic/go-agent/v3/integrations/nrzap"
	newrelic "github.com/newrelic/go-agent/v3/newrelic"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// helper struct

type CoingeckoCoinListResponse struct {
	ID     string `json:"id"`
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

type ETHGasStationGasFeeResponse struct {
	Fast        int     `json:"fast"`
	Fastest     int     `json:"fastest"`
	SafeLow     int     `json:"safeLow"`
	Average     int     `json:"average"`
	BlockTime   float64 `json:"block_time"`
	BlockNum    int     `json:"blockNum"`
	Speed       float64 `json:"speed"`
	SafeLowWait float64 `json:"safeLowWait"`
	AvgWait     float64 `json:"avgWait"`
	FastWait    float64 `json:"fastWait"`
	FastestWait float64 `json:"fastestWait"`
}

type Config struct {
	NewRelicConfig    `json:"newRelicConfig"`
	GasTickerConfig   `json:"gasTickerConfig"`
	PriceTickerConfig `json:"priceTickerConfig"`
}

type NewRelicConfig struct {
	Enabled    bool   `json:"enabled"`
	LicenseKey string `json:"licenseKey"`
	AppName    string `json:"appName"`
}

type GasTickerConfig struct {
	APIKey        string `json:"apiKey"`
	DiscordBotKey string `json:"discordBotKey"`
	GuildID       string `json:"guildID"`
}

type PriceTickerConfig struct {
	CoinList []struct {
		ID            string  `json:"id"`
		CoingeckoID   *string `json:"coingeckoID"`
		DecimalPlace  int     `json:"decimalPlace"`
		VSCurrencies  string  `json:"vsCurrencies"`
		DiscordBotKey string  `json:"discordBotKey"`
		GuildID       string  `json:"guildID"`
	} `json:"coinList"`
}

type Core struct {
	mapMutex   sync.RWMutex
	initStatus bool

	mapper              map[string]string
	sessionPool         map[string]*discordgo.Session
	logger              *zap.SugaredLogger
	config              *Config
	coingeckoClient     *resty.Client
	ethGasStationClient *resty.Client

	newrelicApplication *newrelic.Application
}

func NewCore(cfg *Config, logger *zap.SugaredLogger) (*Core, error) {
	coingeckoClient := resty.New()
	coingeckoClient.HostURL = "https://api.coingecko.com/api/v3"

	ethGasStationClient := resty.New()
	ethGasStationClient.HostURL = "https://ethgasstation.info/api/"

	return &Core{
		sessionPool:         make(map[string]*discordgo.Session),
		initStatus:          false,
		logger:              logger,
		config:              cfg,
		coingeckoClient:     coingeckoClient,
		ethGasStationClient: ethGasStationClient,
	}, nil
}

func (w *Core) AttachNewRelicApplication(app *newrelic.Application) {
	w.newrelicApplication = app
}

func (w *Core) getSession(ctx context.Context, key string) (*discordgo.Session, error) {
	_, txn, logger := w.initializeContext(ctx, "getSession")
	defer txn.StartSegment("getSession").End()
	defer logger.Sync()
	logger.Debug("starting getSession...")

	if _, ok := w.sessionPool[key]; !ok {
		logger.Debug("session not found, trying to initialize connection...")
		client, err := discordgo.New("Bot " + key)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize websocket connection: %w", err)
		}
		logger.Debug("open websocket connection to discord...")
		err = client.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open websocket conenction: %w", err)
		}
		w.sessionPool[key] = client
	}

	return w.sessionPool[key], nil
}

func (w *Core) symbolMapper(s string) string {
	switch s {
	case "usd":
		return "$"
	case "idr":
		return "RP."
	}
	return "$"
}

func (w *Core) IDMapper() error {
	_, txn, logger := w.initializeContext(context.Background(), "IDMapper")
	defer txn.End()
	defer txn.StartSegment("IDMapper").End()
	defer logger.Sync()
	logger.Debug("starting IDMapper...")

	logger.Debug("locking map...")
	w.mapMutex.Lock()
	defer w.mapMutex.Unlock()

	m := map[string]string{}
	logger.Debug("fetching id-symbol from coingecko...")
	resp, err := w.coingeckoClient.R().
		SetResult([]CoingeckoCoinListResponse{}).
		Get("/coins/list")
	if err != nil {
		logger.Error("failed to map id: ", err)
		return err
	} else if resp.IsError() {
		tmperr := fmt.Errorf("failed to map id: %s", string(resp.Body()))
		logger.Errorf(tmperr.Error())
		return tmperr
	}
	idData := resp.Result().(*[]CoingeckoCoinListResponse)

	logger.Debug("mapping id to symbol...")
	for _, data := range *idData {
		m[strings.ToLower(data.Symbol)] = data.ID
	}

	logger.Debug("assign new map to core...")
	w.mapper = m
	w.initStatus = true

	return nil
}

func (w *Core) updateToDiscord(ctx context.Context, guildID string, discordBotKey string, nickname string, status string) error {
	ctx, txn, logger := w.initializeContext(ctx, "updateToDiscord")
	defer txn.StartSegment("updateToDiscord").End()
	defer logger.Sync()
	logger.Debug("starting updateToDiscord...")

	client, err := w.getSession(ctx, discordBotKey)
	if err != nil {
		return fmt.Errorf("failed to get discord session: %w", err)
	}

	logger.Debug("change nickname...")
	err = client.GuildMemberNickname(guildID, "@me", nickname)
	if err != nil {
		return fmt.Errorf("failed to change nickname: %w", err)
	}

	logger.Debug("change status...")
	err = client.UpdateListeningStatus(status)
	if err != nil {
		return fmt.Errorf("failed to change status: %w", err)
	}

	return nil
}

func (w *Core) UpdatePriceTicker() error {
	ctx, txn, logger := w.initializeContext(context.Background(), "UpdatePriceTicker")
	defer txn.End()
	defer logger.Sync()
	logger.Debug("starting UpdatePriceTicker...")

	if !w.initStatus {
		logger.Error("map is not initialized yet!")
		return errors.New("map is not initialized yet")
	}

	w.mapMutex.Lock()
	defer w.mapMutex.Unlock()
	for _, coin := range w.config.PriceTickerConfig.CoinList {
		logger.Debugf("trying to update %s...", coin.ID)
		var ids string
		ok := true
		if coin.CoingeckoID != nil {
			ids = *coin.CoingeckoID
		} else {
			ids, ok = w.mapper[strings.ToLower(coin.ID)]
		}

		if !ok {
			logger.Errorf("failed to find %s", coin.ID)
			continue
		}

		logger.Debug("fetching price...")
		resp, err := w.coingeckoClient.R().
			SetQueryParams(map[string]string{
				"ids":                 ids,
				"vs_currencies":       coin.VSCurrencies,
				"include_24hr_change": "true",
			}).Get("/simple/price")

		if err != nil {
			logger.Errorf("failed to fetch %s: %w", coin.ID, err)
			continue
		} else if resp.IsError() {
			tmperr := fmt.Errorf("failed to fetch %s: %s", coin.ID, string(resp.Body()))
			logger.Errorf(tmperr.Error())
			continue
		}

		logger.Debug("parsing json...")
		jsonParser, err := gabs.ParseJSON(resp.Body())
		if err != nil {
			logger.Error("failed to parse json: ", err)
			continue
		}

		price := strconv.FormatFloat(jsonParser.Path(fmt.Sprintf("%s.usd", ids)).Data().(float64), 'f', coin.DecimalPlace, 64)
		change := strconv.FormatFloat(jsonParser.Path(fmt.Sprintf("%s.usd_24h_change", ids)).Data().(float64), 'f', 2, 64)

		logger.Debug("trying to update discord bot...")
		err = w.updateToDiscord(ctx, coin.GuildID, coin.DiscordBotKey, fmt.Sprintf("%s %s%s", coin.ID, w.symbolMapper(coin.VSCurrencies), price), fmt.Sprintf("24H: %s%%", change))
		if err != nil {
			logger.Error("failed to update to discord: ", err)
			return err
		}
	}
	return nil
}

func (w *Core) UpdateGasTicker() error {
	ctx, txn, logger := w.initializeContext(context.Background(), "UpdateGasTicker")
	defer txn.End()
	defer logger.Sync()
	logger.Debug("starting UpdateGasTicker...")

	resp, err := w.ethGasStationClient.R().
		SetResult(ETHGasStationGasFeeResponse{}).
		SetQueryParams(map[string]string{
			"api-key": w.config.GasTickerConfig.APIKey,
		}).Get("/ethgasAPI.json")
	if err != nil {
		logger.Errorf("failed to fetch gas: %w", err)
		return err
	} else if resp.IsError() {
		tmperr := fmt.Errorf("failed to fetch gas: %s", string(resp.Body()))
		logger.Errorf(tmperr.Error())
		return tmperr
	}

	data := resp.Result().(*ETHGasStationGasFeeResponse)
	nickname := fmt.Sprintf("🚶%d gwei", data.Average/10.0)
	status := fmt.Sprintf("⚡%d 🐌%d", data.Fast/10.0, data.SafeLow/10.0)

	err = w.updateToDiscord(ctx, w.config.GasTickerConfig.GuildID, w.config.GasTickerConfig.DiscordBotKey, nickname, status)
	if err != nil {
		logger.Error("failed to update to discord: ", err)
		return err
	}

	return nil
}

func main() {
	realMain()
}

func realMain() {
	var tmpLog *zap.Logger
	var err error
	isProd := true
	if val := os.Getenv("MODE"); val == "DEBUG" {
		isProd = false
		tmpLog, err = zap.NewDevelopment()
	} else {
		tmpLog, err = zap.NewProduction()
	}
	if err != nil {
		panic("developer is dumb as fuck, can't even initialize log properly")
	}
	logger := tmpLog.Sugar()

	initLogger := logger.Named("Initialization")
	defer initLogger.Sync()

	initLogger.Info("setting variable for viper")
	err = setViper()
	if err != nil {
		initLogger.Error("failed to initialize viper variable: ", err)
		return
	}
	cfg, err := generateConfig()
	if err != nil {
		initLogger.Error("failed to generate config: ", err)
		return
	}

	initLogger.Info("initializing core object...")
	core, err := NewCore(cfg, logger)
	if err != nil {
		initLogger.Fatal("failed to initialize core object: ", err)
		return
	}

	initLogger.Info("initializing new relic application...")
	app, err := newrelic.NewApplication(
		newrelic.ConfigAppName(cfg.NewRelicConfig.AppName),
		newrelic.ConfigLicense(cfg.NewRelicConfig.LicenseKey),
		func(config *newrelic.Config) {
			if isProd {
				if cfg.NewRelicConfig.Enabled {
					initLogger.Info("production mode detected, init for production...")
					config.Enabled = true
					nrzap.ConfigLogger(logger.Named("newrelic").Desugar())
				} else {
					initLogger.Info("production mode detected but new relic is not enabled, continue without new relic...")
				}
			} else {
				config.Enabled = false
				initLogger.Info("development mode detected, newrelic disabled")
			}
		})
	if err != nil {
		initLogger.Fatal("failed to initialize new relic application: ", err)
		return
	}

	core.AttachNewRelicApplication(app)

	core.IDMapper()

	initLogger.Info("invoking cron object...")
	c := gocron.NewScheduler(time.Local).SingletonMode()
	c.Every(1).Hour().Do(core.IDMapper)
	c.Every(1).Minute().Do(core.UpdatePriceTicker)
	c.Every(1).Minute().Do(core.UpdateGasTicker)
	c.StartBlocking()
}

func generateConfig() (*Config, error) {
	var cfg *Config
	err := viper.Unmarshal(&cfg)
	return cfg, err
}

func setViper() error {
	viper.SetConfigName("config")
	viper.SetConfigType("json")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config/")

	return viper.ReadInConfig()
}

type contextKey string

const loggerKey = contextKey("logger")

// WithLogger creates a new context with the provided logger attached.
func (*Core) withLogger(ctx context.Context, logger *zap.SugaredLogger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext returns the logger stored in the context. If no such logger
// exists, a default logger is returned.
func (w *Core) logFromContext(ctx context.Context) *zap.SugaredLogger {
	if logger, ok := ctx.Value(loggerKey).(*zap.SugaredLogger); ok {
		return logger
	}
	return w.logger
}

func (w *Core) initializeContext(ctx context.Context, name string) (context.Context, *newrelic.Transaction, *zap.SugaredLogger) {
	var txn *newrelic.Transaction
	if newrelic.FromContext(ctx) != nil {
		txn = newrelic.FromContext(ctx)
	} else {
		txn = w.newrelicApplication.StartTransaction(name)
		ctx = newrelic.NewContext(ctx, txn)
	}
	ctx = w.withLogger(ctx, w.logger.Named(name))
	logger := w.logFromContext(ctx)
	return ctx, txn, logger
}

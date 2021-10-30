package main

import (
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
	GasTickerConfig   `json:"gasTickerConfig"`
	PriceTickerConfig `json:"priceTickerConfig"`
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

func (w *Core) getSession(key string) (*discordgo.Session, error) {
	logger := w.logger.Named("getSession")
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
	logger := w.logger.Named("IDMapper")
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
		return err
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

func (w *Core) updateToDiscord(guildID string, discordBotKey string, nickname string, status string) error {
	logger := w.logger.Named("updateToDiscord")
	defer logger.Sync()
	logger.Debug("starting updateToDiscord...")

	client, err := w.getSession(discordBotKey)
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
	logger := w.logger.Named("UpdatePriceTicker")
	defer logger.Sync()
	logger.Debug("Starting UpdatePriceTicker...")

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
		err = w.updateToDiscord(coin.GuildID, coin.DiscordBotKey, fmt.Sprintf("%s %s%s", coin.ID, w.symbolMapper(coin.VSCurrencies), price), fmt.Sprintf("24H: %s%%", change))
		if err != nil {
			logger.Error("failed to update to discord: ", err)
			return err
		}
	}
	return nil
}

func (w *Core) UpdateGasTicker() error {
	logger := w.logger.Named("UpdateGasTicker")
	defer logger.Sync()
	logger.Debug("Starting UpdateGasTicker...")

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

	err = w.updateToDiscord(w.config.GasTickerConfig.GuildID, w.config.GasTickerConfig.DiscordBotKey, nickname, status)
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
	if val := os.Getenv("MODE"); val == "DEBUG" {
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

	return viper.ReadInConfig()
}

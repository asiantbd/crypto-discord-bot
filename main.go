package main

import (
	"errors"
	"fmt"
	"os"
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

type Config struct {
	GasTickerConfig   `json:"gasTickerConfig"`
	PriceTickerConfig `json:"priceTickerConfig"`
}

type GasTickerConfig struct{}

type PriceTickerConfig struct {
	CoinList []struct {
		ID            string `json:"id"`
		DecimalPlace  int    `json:"decimalPlace"`
		VSCurrencies  string `json:"vsCurrencies"`
		DiscordBotKey string `json:"discordBotKey"`
		GuildID       string `json:"guildID"`
	} `json:"coinList"`
}

type Core struct {
	mapMutex   sync.RWMutex
	initStatus bool

	mapper          map[string]string
	sessionPool     map[string]*discordgo.Session
	logger          *zap.SugaredLogger
	config          *Config
	coingeckoClient *resty.Client
}

func NewCore(cfg *Config, logger *zap.SugaredLogger) (*Core, error) {
	coingeckoClient := resty.New()
	coingeckoClient.HostURL = "https://api.coingecko.com/api/v3"

	return &Core{
		sessionPool:     make(map[string]*discordgo.Session),
		initStatus:      false,
		logger:          logger,
		config:          cfg,
		coingeckoClient: coingeckoClient,
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
			logger.Error("failed to initialize websocket connection: ", err)
			return nil, err
		}
		logger.Debug("open websocket connection to discord...")
		err = client.Open()
		if err != nil {
			logger.Error("failed to open websocket conenction: ", err)
			return nil, err
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

func (w *Core) IDMapper() {
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
		return
	} else if resp.IsError() {
		logger.Error("failed to map id: ", string(resp.Body()))
		return
	}
	idData := resp.Result().(*[]CoingeckoCoinListResponse)

	logger.Debug("mapping id to symbol...")
	for _, data := range *idData {
		m[strings.ToLower(data.Symbol)] = data.ID
	}

	logger.Debug("assign new map to core...")
	w.mapper = m
	w.initStatus = true
}

func (w *Core) updateToDiscord(guildID string, discordBotKey string, nickname string, status string) error {
	logger := w.logger.Named("updateToDiscord")
	defer logger.Sync()
	logger.Debug("starting updateToDiscord...")

	client, err := w.getSession(discordBotKey)
	if err != nil {
		logger.Error("failed to get discord session: ", err)
		return err
	}

	logger.Debug("change nickname...")
	err = client.GuildMemberNickname(guildID, "@me", nickname)
	if err != nil {
		logger.Error("failed to change nickname: ", err)
		return err
	}

	logger.Debug("change status...")
	err = client.UpdateListeningStatus(status)
	if err != nil {
		logger.Error("failed to change status: ", err)
		return err
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
		ids, ok := w.mapper[strings.ToLower(coin.ID)]
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
		}

		logger.Debug("parsing json...")
		jsonParser, err := gabs.ParseJSON(resp.Body())
		if err != nil {
			logger.Error("failed to parse json: ", err)
			continue
		}

		price := jsonParser.Path(fmt.Sprintf("%s.usd", ids)).Data().(float64)
		change := jsonParser.Path(fmt.Sprintf("%s.usd_24h_change", ids)).Data().(float64)

		logger.Debug("trying to update discord bot...")
		err = w.updateToDiscord(coin.GuildID, coin.DiscordBotKey, fmt.Sprintf("%s %s%f", coin.ID, w.symbolMapper(coin.VSCurrencies), price), fmt.Sprintf("24H: %f%%", change))
		if err != nil {
			logger.Error("failed to update to discord: ", err)
			return err
		}
	}
	return nil
}

func main() {
	realMain()
}

func realMain() {
	var tmpLog *zap.Logger
	var err error
	if val := os.Getenv("MODE"); val == "PRODUCTION" {
		tmpLog, err = zap.NewProduction()
	} else {
		tmpLog, err = zap.NewDevelopment()
	}
	if err != nil {
		panic("developer is dumb as fuck, can't even initialize log properly")
	}
	logger := tmpLog.Sugar()

	initLogger := logger.Named("Initialization")
	defer initLogger.Sync()

	initLogger.Info("setting variable for viper")
	viper.SetConfigName("config")
	viper.SetConfigType("json")
	viper.AddConfigPath(".")

	initLogger.Info("get config values")
	err = viper.ReadInConfig()
	if err != nil {
		initLogger.Fatal("failed to read config file: ", err)
		return
	}
	var cfg *Config
	viper.Unmarshal(&cfg)

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
	c.StartBlocking()
}

package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/eleksir/go-xmpp"
	"github.com/hjson/hjson-go"
	log "github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
)

// Хэндлер сигналов закрывает все бд, все сетевые соединения и сваливает из приложения
func sigHandler() {
	log.Debug("Installing signal handler")

	for s := range sigChan {
		switch s {
		case syscall.SIGINT:
			log.Infoln("Got SIGINT, quitting")
		case syscall.SIGTERM:
			log.Infoln("Got SIGTERM, quitting")
		case syscall.SIGQUIT:
			log.Infoln("Got SIGQUIT, quitting")

		// Заходим на новую итерацию, если у нас "неинтересный" сигнал
		default:
			continue
		}

		// Чтобы не срать в логи ошибками, проставим shutdown state приложения в true
		shutdown = true

		if isConnected && !shutdown {
			log.Debug("Try to set our presence to Unavailable and status to Offline")

			// Вот тут понадобится коллекция известных пользователей, чтобы им разослать presence, что бот свалил в offline
			// Пока за неимением лучшего сообщим об этом самим себе
			for _, room := range roomsConnected {
				if _, err := talk.SendPresence(
					xmpp.Presence{ //nolint:exhaustruct
						To:     room,
						Status: "Offline",
						Type:   "unavailable",
					},
				); err != nil {
					log.Infof("Unable to send presence to jabber server: %s", err)
				}
			}

			// И закрываем соединение
			log.Debugf("Closing connection to jabber server")

			if err := talk.Close(); err != nil {
				log.Infof("Unable to close connection to jabber server: %s", err)
			}
		}

		os.Exit(0)
	}
}

// Читает и валидирует конфиг, а также выставляет некоторые default-ы, если значений для параметров в конфиге нет
func readConfig() {
	configLoaded := false
	executablePath, err := os.Executable()

	if err != nil {
		log.Errorf("Unable to get current executable path: %s", err)
	}

	configJSONPath := fmt.Sprintf("%s/data/config.json", filepath.Dir(executablePath))

	locations := []string{
		"~/.echo-jabber-bot.json",
		"~/echo-jabber-bot.json",
		"/etc/echo-jabber-bot.json",
		configJSONPath,
	}

	for _, location := range locations {
		fileInfo, err := os.Stat(location)

		// Предполагаем, что файла либо нет, либо мы не можем его прочитать, второе надо бы логгировать, но пока забьём
		if err != nil {
			continue
		}

		// Конфиг-файл длинноват для конфига, попробуем следующего кандидата
		if fileInfo.Size() > 65535 {
			log.Warnf("Config file %s is too long for config, skipping", location)

			continue
		}

		buf, err := os.ReadFile(location)

		// Не удалось прочитать, попробуем следующего кандидата
		if err != nil {
			log.Warnf("Skip reading config file %s: %s", location, err)

			continue
		}

		// Исходя из документации, hjson какбы умеет парсить "кривой" json, но парсит его в map-ку.
		// Интереснее на выходе получить структурку: то есть мы вначале конфиг преобразуем в map-ку, затем эту map-ку
		// сериализуем в json, а потом json преврщааем в стркутурку. Не очень эффективно, но он и не часто требуется.
		var sampleConfig myConfig
		var tmp map[string]interface{}
		err = hjson.Unmarshal(buf, &tmp)

		// Не удалось распарсить - попробуем следующего кандидата
		if err != nil {
			log.Warnf("Skip parsing config file %s: %s", location, err)

			continue
		}

		tmpjson, err := json.Marshal(tmp)

		// Не удалось преобразовать map-ку в json
		if err != nil {
			log.Warnf("Skip parsing config file %s: %s", location, err)

			continue
		}

		if err := json.Unmarshal(tmpjson, &sampleConfig); err != nil {
			log.Warnf("Skip parsing config file %s: %s", location, err)

			continue
		}

		// Значения для Jabber-клиента
		if sampleConfig.Jabber.Server == "" {
			sampleConfig.Jabber.Server = "localhost"
			log.Errorf("Jabber server is not defined in config, using localhost")
		}

		if sampleConfig.Jabber.Port == 0 {
			sampleConfig.Jabber.Port = 5552

			if sampleConfig.Jabber.Ssl {
				if !sampleConfig.Jabber.StartTLS {
					sampleConfig.Jabber.Port = 5553
				}
			}

			log.Infof("Jabber port is not defined in config, using 6667")
		}

		if !sampleConfig.Jabber.Ssl {
			sampleConfig.Jabber.StartTLS = false
		}

		if !sampleConfig.Jabber.Ssl || !sampleConfig.Jabber.StartTLS {
			sampleConfig.Jabber.SslVerify = false
		}

		// sampleConfig.Jabber.InsecureAllowUnencryptedAuth = false, если не задан

		if sampleConfig.Jabber.ConnectionTimeout == 0 {
			sampleConfig.Jabber.ConnectionTimeout = 10
		}

		if sampleConfig.Jabber.ReconnectDelay == 0 {
			sampleConfig.Jabber.ReconnectDelay = 3
		}

		if sampleConfig.Jabber.ServerPingDelay == 0 {
			sampleConfig.Jabber.ServerPingDelay = 60
		}

		if sampleConfig.Jabber.MucPingDelay == 0 {
			sampleConfig.Jabber.MucPingDelay = 900
		}

		if sampleConfig.Jabber.MucRejoinDelay == 0 {
			sampleConfig.Jabber.MucRejoinDelay = 3
		}

		if sampleConfig.Jabber.PingSplayDelay == 0 {
			sampleConfig.Jabber.PingSplayDelay = 3
		}

		if sampleConfig.Jabber.Nick == "" {
			log.Errorf("Jabber nick is not defined in config, quitting")
			os.Exit(1)
		}

		if sampleConfig.Jabber.User == "" {
			sampleConfig.Jabber.User = fmt.Sprintf("%s@%s", sampleConfig.Jabber.Nick, sampleConfig.Jabber.Server)
		}

		// Если sampleConfig.Jabber.Password не задан, то авторизации через Nickserv или SASL не будет
		// Если sampleConfig.Jabber.Sasl не задан, то авторизация происходит через NickServ

		// Нам бот нужен в каких-то чат-румах, а не "просто так"
		if len(sampleConfig.Jabber.Channels) < 1 {
			log.Errorf("No jabber channels/rooms defined in config, quitting")
			os.Exit(1)
		}

		if sampleConfig.Loglevel == "" {
			sampleConfig.Loglevel = "info"
		}

		// sampleConfig.Log = "" if not set

		config = sampleConfig
		configLoaded = true
		log.Infof("Using %s as config file", location)

		break
	}

	if !configLoaded {
		log.Error("Config was not loaded! Refusing to start.")
		os.Exit(1)
	}
}

func establishConnection() {
	var err error

	if connecting && !isConnected {
		return
	}

	// проставляем глобальные переменные
	connecting = true
	isConnected = false
	roomsConnected = make([]string, 0)

	for {
		talk, err = options.NewClient()

		if err != nil {
			log.Error(err)

			reEstablishConnection()
		} else {
			break
		}
	}

	// По идее keepalive должен же проходить только, если мы уже на сервере, так?
	for {
		if _, err := talk.SendKeepAlive(); err != nil {
			log.Errorf("Try to send initial KeepAlive, got error: %s", err)
			time.Sleep(time.Duration(config.Jabber.ReconnectDelay) * time.Second)

			// Понятно, что мы уже не в процессе установления соединения, а произошла ошибка
			connecting = false
			reEstablishConnection()
		} else {
			log.Info("Connected")

			break
		}
	}

	// Джойнимся к чятикам, но делаем это в фоне, чтобы не блочиться на ошибках, например, если бота забанили
	for _, room := range config.Jabber.Channels {
		go joinMuc(room)
	}

	if _, err := talk.SendPresence(
		xmpp.Presence{ //nolint:exhaustruct
			Status: "Ready to chat",
		},
	); err != nil {
		log.Infof("Unable to send presence to jabber server: %s", err)

		reEstablishConnection()
	}

	lastActivity = time.Now().Unix()
	connecting = false
	isConnected = true

	log.Debugf("Sending disco#info to %s", config.Jabber.Server)

	_, err = talk.DiscoverInfo(talk.JID(), config.Jabber.Server)

	if err != nil {
		log.Infof("Unable to send disco#info to jabber server: %s", err)

		reEstablishConnection()
	}
}

func reEstablishConnection() {
	// Серверные capabilities нам *уже* неизвестны
	serverCapsQueried = false
	serverCapsList = NewCollection()
	mucCapsList = NewCollection()
	lastMucActivity = NewCollection()

	// Пока соединение не установлено, мы про пинги ничего не знаем
	serverPingTimestampRx = time.Now().Unix() // Считаем, что если коннект запустился, то первый пинг успешен
	serverPingTimestampTx = 0
	time.Sleep(time.Duration(config.Jabber.ReconnectDelay) * time.Second)
	connecting = false
	establishConnection()
}

// Джойнится к конференциям/каналам/комнатам в джаббере
func joinMuc(room string) {
	log.Debugf("Sending disco#info from %s to %s", talk.JID(), room)

	if _, err := talk.DiscoverInfo(talk.JID(), room); err != nil {
		log.Infof("Unable to send disco#info to MUC %s: %s", room, err)

		reEstablishConnection()
	}

	// Ждём, пока muc нам вернёт список фичей
	for i := 0; i < (20 * int(config.Jabber.ConnectionTimeout)); i++ {
		var myRoom interface{}
		var supported bool
		var exist bool

		time.Sleep(50 * time.Millisecond)

		if myRoom, exist = mucCapsList.Get(room); !exist {
			// Пока не задискаверилась
			continue
		}

		if supported, exist = myRoom.(map[string]bool)["muc_unsecured"]; exist {
			if supported {
				break
			} else {
				log.Infof("Unable to join to password-protected room. Don't know how to enter passwords :)")

				return
			}
		}
	}

	if _, err := talk.JoinMUCNoHistory(room, config.Jabber.Nick); err != nil {
		log.Errorf("Unable to join to MUC: %s", room)

		reEstablishConnection()
	} else {
		log.Infof("Joining to MUC: %s", room)
	}

	// Ждём, когда прилетит presence из комнаты, тогда мы точно знаем, что мы вошли
	for i := 0; i < (20 * int(config.Jabber.ConnectionTimeout)); i++ {
		time.Sleep(50 * time.Millisecond)

		if slices.Contains(roomsConnected, room) {
			break
		}
	}

	// Вот теперь точно можно слать статус
	log.Infof("Joined to MUC: %s", room)

	if _, err := talk.SendPresence(
		xmpp.Presence{ //nolint:exhaustruct
			To:     room,
			Status: "Ready to chat",
		},
	); err != nil {
		log.Infof("Unable to send presence to MUC %s: %s", room, err)
		reEstablishConnection()
	}
}

// Проверяет живость соединения с сервером
func probeServerLiveness() {
	for {
		if shutdown {
			return
		}

		sleepTime := time.Duration(config.Jabber.ServerPingDelay) * 1000 * time.Millisecond
		sleepTime += time.Duration(rand.Int63n(1000*config.Jabber.PingSplayDelay)) * time.Millisecond //nolint:gosec
		time.Sleep(sleepTime)

		if !isConnected {
			continue
		}

		// Пингуем, только если не было никакой активности в течение > config.Jabber.ServerPingDelay,
		// в худшем случе это будет ~ (config.Jabber.PingSplayDelay * 2) + config.Jabber.PingSplayDelay
		if (time.Now().Unix() - lastServerActivity) < (config.Jabber.ServerPingDelay + config.Jabber.PingSplayDelay) {
			continue
		}

		if serverCapsQueried { // Сервер ответил на disco#info
			var value interface{}
			var exist bool

			value, exist = serverCapsList.Get("urn:xmpp:ping")

			switch {
			// Сервер анонсировал, что умеет в c2s пинги
			case exist && value.(bool):
				// Таймаут c2s пинга. Возьмём сумму задержки между пингами, добавим таймаут коннекта и добавим
				// максимальную корректировку разброса.
				txTimeout := config.Jabber.ServerPingDelay + config.Jabber.ConnectionTimeout
				txTimeout += config.Jabber.PingSplayDelay
				rxTimeout := txTimeout

				rxTimeAgo := time.Now().Unix() - serverPingTimestampRx

				if serverPingTimestampTx > 0 { // Первая пуля от нас ушла...
					switch {
					// Давненько мы не получали понгов от сервера, вероятно, соединение с сервером утеряно?
					case rxTimeAgo > (rxTimeout * 2):
						log.Debugf(
							"Stall connection detected. No c2s pong for %d seconds",
							rxTimeAgo,
						)

						reEstablishConnection()

					// По-умолчанию, мы отправляем c2s пинг
					default:
						log.Debugf("Sending c2s ping from %s to %s", talk.JID(), config.Jabber.Server)

						if err := talk.PingC2S(talk.JID(), config.Jabber.Server); err != nil {
							reEstablishConnection()
						} else {
							serverPingTimestampTx = time.Now().Unix()
						}
					}
				} else { // Первая пуля пока не вылетела, отправляем
					log.Debugf("Sending first c2s ping from %s to %s", talk.JID(), config.Jabber.Server)

					if err := talk.PingC2S(talk.JID(), config.Jabber.Server); err != nil {
						reEstablishConnection()
					} else {
						serverPingTimestampTx = time.Now().Unix()
					}
				}

			// Сервер не анонсировал, что умеет в c2s пинги
			default:
				log.Debug("Sending keepalive whitespace ping")

				if _, err := talk.SendKeepAlive(); err != nil {
					reEstablishConnection()

					continue
				}
			}
		} else { // Сервер не ответил на disco#info
			log.Debug("Sending keepalive whitespace ping")

			if _, err := talk.SendKeepAlive(); err != nil {
				reEstablishConnection()

				continue
			}
		}
	}
}

// Пингует MUC-и, нужно для проверки, что клиент ещё находится в MUC-е
func probeMUCLiveness() {
	for {
		for _, room := range roomsConnected {
			var exist bool
			var lastActivityTs interface{}

			// Если записи про комнату нету, то пинговать её бессмысленно.
			if lastActivityTs, exist = lastMucActivity.Get(room); !exist {
				continue
			}

			// Если время последней активности в чятике не превысило
			// config.Jabber.ServerPingDelay + config.Jabber.PingSplayDelay, ничего не пингуем
			if (time.Now().Unix() - lastActivityTs.(int64)) < (config.Jabber.ServerPingDelay + config.Jabber.PingSplayDelay) {
				continue
			}

			/* Пинг MUC-а по сценарию без серверной оптимизации мы реализовывать не будем. Это как-то не надёжно.
			go func(room string) {
				// Небольшая рандомная задержка перед пингом комнаты
				sleepTime := time.Duration(rand.Int63n(1000*config.Jabber.PingSplayDelay)) * time.Millisecond //nolint:gosec
				time.Sleep(sleepTime)

				if err := talk.PingS2S(talk.JID(), room+"/"+config.Jabber.Nick); err != nil {
					reEstablishConnection()
				}
			}(room)
			*/

			var roomMap interface{}

			roomMap, exist = mucCapsList.Get(room)

			// Пинги комнаты проводим, только если она записана, как прошедшая disco#info и поддерживающая
			// Server Optimization
			if exist && roomMap.(map[string]bool)["http://jabber.org/protocol/muc#self-ping-optimization"] {
				go func(room string) {
					// Небольшая рандомная задержка перед пингом комнаты
					sleepTime := time.Duration(rand.Int63n(1000*config.Jabber.PingSplayDelay)) * time.Millisecond //nolint:gosec
					time.Sleep(sleepTime)

					log.Debugf("Sending MUC ping from %s to %s", talk.JID(), room)

					if err := talk.PingS2S(talk.JID(), room); err != nil {
						reEstablishConnection()
					}
				}(room)
			}
		}

		time.Sleep(time.Duration(config.Jabber.MucPingDelay) * time.Second)
	}
}

/* vim: set ft=go noet ai ts=4 sw=4 sts=4: */

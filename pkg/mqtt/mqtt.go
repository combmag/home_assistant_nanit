package mqtt

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/combmag/home_assistant_nanit/pkg/baby"
	CLT "github.com/combmag/home_assistant_nanit/pkg/client"
	"github.com/combmag/home_assistant_nanit/pkg/utils"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

// Connection - MQTT context
type Connection struct {
	Opts         Opts
	StateManager *baby.StateManager
}

// VOlume MQTT Topic Data
type VolumeMessage struct {
	Volume int `json:"volume"`
}

type LightMessage struct {
	IsLightOn         bool   `json:"isLightOn"`
	NightLightTimeout *int32 `json:"nightLightTimeout"`
	Brightness        *int32 `json:"brightness"`
}

// NewConnection - constructor
func NewConnection(opts Opts) *Connection {
	return &Connection{
		Opts: opts,
	}
}

// Run - runs the mqtt connection handler
func (conn *Connection) Run(manager *baby.StateManager, ctx utils.GracefulContext) {
	conn.StateManager = manager

	utils.RunWithPerseverance(func(attempt utils.AttemptContext) {
		runMqtt(conn, attempt)
	}, ctx, utils.PerseverenceOpts{
		RunnerID:       "mqtt",
		ResetThreshold: 2 * time.Second,
		Cooldown: []time.Duration{
			2 * time.Second,
			10 * time.Second,
			1 * time.Minute,
		},
	})
}

// {id:473  type:PUT_CONTROL  control:{nightLight:LIGHT_ON}}
func handleLight(client MQTT.Client, msg MQTT.Message) {
	log.Info().Str("CONN NULL", "Entered")
	var connection = CLT.GetWebsocketConnection()
	if connection == nil {
		log.Info().Str("CONN NULL", "NULL CONNECTION")
		return
	}
	log.Info().Str("topic", msg.Topic()).Msgf("Received Volume: %s", msg.Payload())
	var lightResult LightMessage
	if err := json.Unmarshal(msg.Payload(), &lightResult); err != nil {
		log.Printf("Error parsing JSON: %v\n", err)
		return
	}
	var lightStatus CLT.Control_NightLight
	if lightResult.IsLightOn {
		lightStatus = CLT.Control_LIGHT_ON
	} else {
		lightStatus = CLT.Control_LIGHT_OFF
	}
	var settingsRequest = &CLT.Request{
		Control: &CLT.Control{
			NightLight: &lightStatus,
		},
	}

	awaitFunc := connection.SendRequest(CLT.RequestType_PUT_CONTROL, settingsRequest)

	// Wait for the response with a timeout (e.g., 5 seconds)
	response, err := awaitFunc(60 * time.Second)
	if err != nil {
		log.Error().Err(err).Msg("Error while awaiting light response")
	} else {
		log.Info().Interface("response", response).Msg("Light response received")
	}

	if lightResult.NightLightTimeout != nil {

		var timer = lightResult.NightLightTimeout
		var timerRequest = &CLT.Request{
			Control: &CLT.Control{
				NightLightTimeout: timer,
			},
		}

		awaitFunc = connection.SendRequest(CLT.RequestType_PUT_CONTROL, timerRequest)

		// Wait for the response with a timeout (e.g., 5 seconds)
		response, err = awaitFunc(5 * time.Second)
		if err != nil {
			log.Error().Err(err).Msg("Error while awaiting light Timeout response")
		} else {
			log.Info().Interface("response", response).Msg("Light Timeout response received")
		}
	}

	if lightResult.Brightness != nil {
		var brightness = lightResult.Brightness
		var brightnessRequest = &CLT.Request{
			Settings: &CLT.Settings{
				Brightness: brightness,
			},
		}

		awaitFunc = connection.SendRequest(CLT.RequestType_PUT_SETTINGS, brightnessRequest)

		// Wait for the response with a timeout (e.g., 5 seconds)
		response, err = awaitFunc(5 * time.Second)
		if err != nil {
			log.Error().Err(err).Msg("Error while awaiting light brightness response")
		} else {
			log.Info().Interface("response", response).Msg("Light brightness response received")
		}
	}
}

//	{
//	  "volume": 30
//	}
func handleVolume(client MQTT.Client, msg MQTT.Message) {
	log.Info().Str("topic", msg.Topic()).Msgf("Received Volume: %s", msg.Payload())
	var connection = CLT.GetWebsocketConnection()
	if connection == nil {
		return
	}
	var volumeResult VolumeMessage
	if err := json.Unmarshal(msg.Payload(), &volumeResult); err != nil {
		log.Printf("Error parsing JSON: %v\n", err)
		return
	}
	messageVolume := int32(volumeResult.Volume)
	var settingsRequest = &CLT.Request{
		Settings: &CLT.Settings{},
	}
	settingsRequest.Settings.Volume = &messageVolume

	awaitFunc := connection.SendRequest(CLT.RequestType_PUT_SETTINGS, settingsRequest)

	// Wait for the response with a timeout (e.g., 5 seconds)
	response, err := awaitFunc(60 * time.Second)
	if err != nil {
		log.Error().Err(err).Msg("Error while awaiting volume response")
	} else {
		log.Info().Interface("response", response).Msg("Volume response received")
	}
}
func handlePlayback(client MQTT.Client, msg MQTT.Message) {
	log.Info().Str("topic", msg.Topic()).Msgf("Received message: %s", msg.Payload())
	var connection = CLT.GetWebsocketConnection()
	if connection == nil {
		return
	}
	// Parse the MQTT message payload (assuming it's JSON)
	var mqttPayload struct {
		Playback struct {
			Status       string `json:"status"`
			PlayBackType string `json:"playbackType"`
			Duration     *int32 `json:"duration"`
		} `json:"playback"`
	}
	if err := json.Unmarshal(msg.Payload(), &mqttPayload); err != nil {
		log.Error().Err(err).Msg("Unable to parse MQTT message")
		return
	}
	// Map the parsed data to the protobuf request
	playbackStatus := mqttPayload.Playback.Status

	// Build the protobuf request based on the status received
	var playbackStatusEnum CLT.Playback_Status
	var playbackRequest *CLT.Request
	switch playbackStatus {
	case "STOPPED":
		playbackStatusEnum = CLT.Playback_STOPPED
		playbackRequest = &CLT.Request{
			Playback: &CLT.Playback{
				Status: &playbackStatusEnum,
			},
		}
	case "STARTED":
		playbackStatusEnum = CLT.Playback_STARTED
		filename := mqttPayload.Playback.PlayBackType
		duration := int32(3600) // Default value
		if mqttPayload.Playback.Duration != nil {
			duration = *mqttPayload.Playback.Duration
		}
		playbackRequest = &CLT.Request{
			Playback: &CLT.Playback{
				Status:   &playbackStatusEnum,
				Duration: &duration,
				Soundtrack: &CLT.Soundtrack{
					Filename: &filename,
					Storage:  CLT.SoundtrackStorage_FACTORY.Enum(),
				},
			},
		}
	default:
		log.Error().Str("status", playbackStatus).Msg("Invalid playback status")
		return
	}
	awaitFunc := connection.SendRequest(CLT.RequestType_PUT_PLAYBACK, playbackRequest)

	// Wait for the response with a timeout (e.g., 5 seconds)
	response, err := awaitFunc(5 * time.Second)
	if err != nil {
		log.Error().Err(err).Msg("Error while awaiting playback response")
	} else {
		log.Info().Interface("response", response).Msg("Playback response received")
	}
}

func runMqtt(conn *Connection, attempt utils.AttemptContext) {
	opts := MQTT.NewClientOptions()
	opts.AddBroker(conn.Opts.BrokerURL)
	opts.SetClientID(conn.Opts.TopicPrefix)
	opts.SetUsername(conn.Opts.Username)
	opts.SetPassword(conn.Opts.Password)
	opts.SetCleanSession(false)

	// Enable auto-reconnect
	opts.SetAutoReconnect(true)

	// Handler to execute on initial connection and reconnection
	opts.SetOnConnectHandler(func(c MQTT.Client) {
		log.Info().Str("broker_url", conn.Opts.BrokerURL).Msg("Connected to MQTT broker")

		// Subscribe to topics upon connection or reconnection
		topicToSubscribe := fmt.Sprintf("%v/babies/playback", conn.Opts.TopicPrefix)
		if token := c.Subscribe(topicToSubscribe, 0, handlePlayback); token.Wait() && token.Error() != nil {
			log.Error().Err(token.Error()).Msg("Failed to subscribe to playback topic")
		}

		topicToSubscribe2 := fmt.Sprintf("%v/babies/volume", conn.Opts.TopicPrefix)
		if token := c.Subscribe(topicToSubscribe2, 0, handleVolume); token.Wait() && token.Error() != nil {
			log.Error().Err(token.Error()).Msg("Failed to subscribe to volume topic")
		}

		topicToSubscribe3 := fmt.Sprintf("%v/babies/light", conn.Opts.TopicPrefix)
		if token := c.Subscribe(topicToSubscribe3, 0, handleLight); token.Wait() && token.Error() != nil {
			log.Error().Err(token.Error()).Msg("Failed to subscribe to light topic")
		}
	})

	// Handler to log connection lost events
	opts.SetConnectionLostHandler(func(client MQTT.Client, err error) {
		log.Warn().Err(err).Msg("MQTT connection lost, attempting to reconnect")
	})

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Error().Str("broker_url", conn.Opts.BrokerURL).Err(token.Error()).Msg("Unable to connect to MQTT broker")
		attempt.Fail(token.Error())
		return
	}

	// Publish state updates
	unsubscribe := conn.StateManager.Subscribe(func(babyUID string, state baby.State) {
		publish := func(key string, value interface{}) {
			topic := fmt.Sprintf("%v/babies/%v/%v", conn.Opts.TopicPrefix, babyUID, key)
			log.Trace().Str("topic", topic).Interface("value", value).Msg("MQTT publish")

			token := client.Publish(topic, 0, false, fmt.Sprintf("%v", value))
			if token.Wait(); token.Error() != nil {
				log.Error().Err(token.Error()).Msgf("Unable to publish %v update", key)
			}
		}

		for key, value := range state.AsMap(false) {
			publish(key, value)
		}

		if state.StreamState != nil && *state.StreamState != baby.StreamState_Unknown {
			publish("is_stream_alive", *state.StreamState == baby.StreamState_Alive)
		}
	})

	// Wait until interrupt signal is received
	<-attempt.Done()

	log.Debug().Msg("Closing MQTT connection on interrupt")
	unsubscribe()
	client.Disconnect(250)
}

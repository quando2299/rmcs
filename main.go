package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/pion/webrtc/v4"
)

const (
	broker     = "test.rmcs.d6-vnext.com"
	port       = 1883
	username   = "vnext-test_b6239876-943a-4d6f-a7ef-f1440d5c58af"
	password   = "7#TlDprf"
	thingName  = "vnext-test_b6239876-943a-4d6f-a7ef-f1440d5c58af"
	clientID   = "go-backend-rmcs-client"
	baseTopic  = "vnext-test_b6239876-943a-4d6f-a7ef-f1440d5c58af/robot-control"
)

func main() {
	mqtt.ERROR = log.New(os.Stdout, "[ERROR] ", 0)
	mqtt.CRITICAL = log.New(os.Stdout, "[CRITICAL] ", 0)
	mqtt.WARN = log.New(os.Stdout, "[WARN] ", 0)
	mqtt.DEBUG = log.New(os.Stdout, "[DEBUG] ", 0)

	// Initialize WebRTC manager
	webrtcManager, err := NewWebRTCManager()
	if err != nil {
		log.Fatalf("Failed to create WebRTC manager: %v", err)
	}
	defer webrtcManager.Close()

	// Store current peer ID from offer topic
	var currentPeerID string


	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", broker, port))
	opts.SetClientID(clientID)
	opts.SetUsername(username)
	opts.SetPassword(password)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)

	// No TLS for this broker
	// tlsConfig := &tls.Config{
	// 	InsecureSkipVerify: false,
	// }
	// opts.SetTLSConfig(tlsConfig)

	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Println("Connected to MQTT Broker successfully!")


		// Setup ICE candidate handler to send backend's candidates to frontend
		webrtcManager.SetupICECandidateHandler(func(candidate *webrtc.ICECandidate) {
			if candidate == nil {
				return
			}

			// Convert to JSON array format (Flutter expects array)
			candidateJSON := []map[string]interface{}{
				{
					"candidate":     candidate.ToJSON().Candidate,
					"sdpMid":        candidate.ToJSON().SDPMid,
					"sdpMLineIndex": candidate.ToJSON().SDPMLineIndex,
				},
			}

			payload, err := json.Marshal(candidateJSON)
			if err != nil {
				log.Printf("Failed to marshal ICE candidate: %v", err)
				return
			}

			// Send to frontend via rmcs candidate topic
			if currentPeerID != "" {
				topic := fmt.Sprintf("%s/%s/candidate/rmcs", baseTopic, currentPeerID)
				token := client.Publish(topic, 0, false, payload)
				if token.Wait() && token.Error() != nil {
					log.Printf("Failed to send ICE candidate: %v", token.Error())
				} else {
					log.Printf("Sent ICE candidate to frontend on topic: %s", topic)
				}
			}
		})

		// Subscribe to offer topic to receive offers from frontend
		offerTopic := fmt.Sprintf("%s/+/offer", baseTopic)
		token := client.Subscribe(offerTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
			log.Printf("Offer received on topic %s: %s", msg.Topic(), string(msg.Payload()))

			// Extract peer ID from topic
			topicParts := make([]string, 0)
			for _, part := range []byte(msg.Topic()) {
				topicParts = append(topicParts, string(part))
			}
			topicStr := string(msg.Topic())
			// Parse topic to get peer ID: baseTopic/peerId/offer
			baseLen := len(baseTopic) + 1 // +1 for the /
			if len(topicStr) > baseLen {
				remainingTopic := topicStr[baseLen:]
				// Find the next /
				for i, ch := range remainingTopic {
					if ch == '/' {
						currentPeerID = remainingTopic[:i]
						break
					}
				}
			}
			log.Printf("Extracted peer ID: %s", currentPeerID)

			// The offer is sent as plain SDP string from Flutter
			offerSDP := string(msg.Payload())

			// Process the offer and create an answer using real WebRTC
			answerSDP, err := webrtcManager.ProcessOffer(offerSDP)
			if err != nil {
				log.Printf("Failed to process offer: %v", err)
				return
			}

			// Send the answer as plain SDP string (Flutter expects plain string)
			answerTopic := fmt.Sprintf("%s/%s/answer", baseTopic, currentPeerID)
			token := client.Publish(answerTopic, 0, false, []byte(answerSDP))
			if token.Wait() && token.Error() != nil {
				log.Printf("Failed to send answer: %v", token.Error())
			} else {
				log.Printf("Real WebRTC answer sent successfully to topic: %s", answerTopic)
			}
		})

		// Subscribe to robot ICE candidate topic
		robotCandidateTopic := fmt.Sprintf("%s/+/candidate/robot", baseTopic)
		iceToken := client.Subscribe(robotCandidateTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
			log.Printf("ICE candidate received on topic %s: %s", msg.Topic(), string(msg.Payload()))

			// Flutter sends ICE candidates as JSON array
			var iceCandidates []ICECandidateMessage
			if err := json.Unmarshal(msg.Payload(), &iceCandidates); err != nil {
				log.Printf("Failed to parse ICE candidates: %v", err)
				return
			}

			// Add each ICE candidate
			for _, iceMsg := range iceCandidates {
				if err := webrtcManager.AddICECandidate(iceMsg); err != nil {
					log.Printf("Failed to add ICE candidate: %v", err)
				}
			}
		})

		if token.Wait() && token.Error() != nil {
			log.Printf("Failed to subscribe to %s: %v", offerTopic, token.Error())
		} else {
			log.Printf("Subscribed to topic: %s", offerTopic)
		}

		if iceToken.Wait() && iceToken.Error() != nil {
			log.Printf("Failed to subscribe to %s: %v", robotCandidateTopic, iceToken.Error())
		} else {
			log.Printf("Subscribed to topic: %s", robotCandidateTopic)
		}
	})

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("Connection lost: %v", err)
	})

	opts.SetReconnectingHandler(func(client mqtt.Client, opts *mqtt.ClientOptions) {
		log.Println("Attempting to reconnect...")
	})

	client := mqtt.NewClient(opts)

	log.Printf("Connecting to MQTT broker at %s:%d...", broker, port)

	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", token.Error())
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Listening for messages. Press Ctrl+C to exit...")

	<-sigChan

	log.Println("Shutting down...")
	client.Disconnect(250)
	log.Println("Disconnected. Goodbye!")
}
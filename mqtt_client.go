package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/pion/webrtc/v4"
)

type MQTTClient struct {
	client        mqtt.Client
	webrtcManager *WebRTCManager
	currentPeerIDs map[string]bool
	mu            sync.Mutex
}

func NewMQTTClient(webrtcManager *WebRTCManager) *MQTTClient {
	return &MQTTClient{
		webrtcManager:  webrtcManager,
		currentPeerIDs: make(map[string]bool),
	}
}

func (m *MQTTClient) Connect() error {
	mqtt.ERROR = log.New(log.Writer(), "[ERROR] ", 0)

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", broker, port))
	opts.SetClientID(clientID)
	opts.SetUsername(username)
	opts.SetPassword(password)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)

	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Println("Connected to MQTT Broker successfully!")

		// Subscribe to camera topic to handle camera switching
		cameraTopic := fmt.Sprintf("%s/camera", thingName)
		cameraToken := client.Subscribe(cameraTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
			log.Printf("Camera switch request received on topic %s: %s", msg.Topic(), string(msg.Payload()))

			// Parse camera number from message
			var cameraNumber int
			_, err := fmt.Sscanf(string(msg.Payload()), "%d", &cameraNumber)
			if err != nil {
				log.Printf("Failed to parse camera number from message: %v", err)
				return
			}

			log.Printf("Parsed camera number: %d", cameraNumber)

			// Switch to requested camera
			if err := m.webrtcManager.SwitchCamera(cameraNumber); err != nil {
				log.Printf("Failed to switch camera: %v", err)
			} else {
				log.Printf("Successfully switched to camera %d", cameraNumber)
			}
		})

		if cameraToken.Wait() && cameraToken.Error() != nil {
			log.Printf("Failed to subscribe to %s: %v", cameraTopic, cameraToken.Error())
		} else {
			log.Printf("Subscribed to camera topic: %s", cameraTopic)
		}

		// Subscribe to disconnect-client topic
		disconnectTopic := fmt.Sprintf("%s/+/disconnect-client", baseTopic)
		disconnectToken := client.Subscribe(disconnectTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
			log.Printf("Disconnect request received on topic %s", msg.Topic())

			// Extract peer ID from topic
			topicStr := string(msg.Topic())
			baseLen := len(baseTopic) + 1
			if len(topicStr) > baseLen {
				remainingTopic := topicStr[baseLen:]
				for i, ch := range remainingTopic {
					if ch == '/' {
						peerID := remainingTopic[:i]
						log.Printf("Disconnecting peer: %s", peerID)

						// Disconnect the peer
						if err := m.webrtcManager.DisconnectPeer(peerID); err != nil {
							log.Printf("Failed to disconnect peer %s: %v", peerID, err)
						}

						// Remove from tracked peers
						m.mu.Lock()
						delete(m.currentPeerIDs, peerID)
						m.mu.Unlock()
						break
					}
				}
			}
		})

		if disconnectToken.Wait() && disconnectToken.Error() != nil {
			log.Printf("Failed to subscribe to %s: %v", disconnectTopic, disconnectToken.Error())
		} else {
			log.Printf("Subscribed to disconnect topic: %s", disconnectTopic)
		}

		// Subscribe to offer topic to receive offers from frontend
		offerTopic := fmt.Sprintf("%s/+/offer", baseTopic)
		token := client.Subscribe(offerTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
			log.Printf("Offer received on topic %s", msg.Topic())

			// Extract peer ID from topic
			topicStr := string(msg.Topic())
			// Parse topic to get peer ID: baseTopic/peerId/offer
			baseLen := len(baseTopic) + 1 // +1 for the /
			if len(topicStr) > baseLen {
				remainingTopic := topicStr[baseLen:]
				// Find the next /
				for i, ch := range remainingTopic {
					if ch == '/' {
						peerID := remainingTopic[:i]
						log.Printf("Extracted peer ID: %s", peerID)

						// Track this peer
						m.mu.Lock()
						m.currentPeerIDs[peerID] = true
						m.mu.Unlock()

			// The offer is sent as plain SDP string from Flutter
			offerSDP := string(msg.Payload())

						// Process the offer and create an answer using real WebRTC
						answerSDP, err := m.webrtcManager.ProcessOffer(peerID, offerSDP)
						if err != nil {
							log.Printf("Failed to process offer: %v", err)
							return
						}

						// Setup ICE candidate handler for this peer
						m.webrtcManager.SetupICECandidateHandler(peerID, func(candidate *webrtc.ICECandidate) {
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
							topic := fmt.Sprintf("%s/%s/candidate/rmcs", baseTopic, peerID)
							token := client.Publish(topic, 0, false, payload)
							if token.Wait() && token.Error() != nil {
								log.Printf("Failed to send ICE candidate: %v", token.Error())
							} else {
								log.Printf("Sent ICE candidate to frontend on topic: %s", topic)
							}
						})

						// Send the answer as plain SDP string (Flutter expects plain string)
						answerTopic := fmt.Sprintf("%s/%s/answer", baseTopic, peerID)
						token := client.Publish(answerTopic, 0, false, []byte(answerSDP))
						if token.Wait() && token.Error() != nil {
							log.Printf("Failed to send answer: %v", token.Error())
						}
						break
					}
				}
			}
		})

		// Subscribe to robot ICE candidate topic
		robotCandidateTopic := fmt.Sprintf("%s/+/candidate/robot", baseTopic)
		iceToken := client.Subscribe(robotCandidateTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
			// Flutter sends ICE candidates as JSON array
			var iceCandidates []ICECandidateMessage
			if err := json.Unmarshal(msg.Payload(), &iceCandidates); err != nil {
				log.Printf("Failed to parse ICE candidates: %v", err)
				return
			}

			// Extract peer ID from topic
			topicStr := string(msg.Topic())
			baseLen := len(baseTopic) + 1
			if len(topicStr) > baseLen {
				remainingTopic := topicStr[baseLen:]
				for i, ch := range remainingTopic {
					if ch == '/' {
						peerID := remainingTopic[:i]
						// Add each ICE candidate
						for _, iceMsg := range iceCandidates {
							if err := m.webrtcManager.AddICECandidate(peerID, iceMsg); err != nil {
								log.Printf("Failed to add ICE candidate: %v", err)
							}
						}
						break
					}
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

	m.client = mqtt.NewClient(opts)

	log.Printf("Connecting to MQTT broker at %s:%d...", broker, port)

	if token := m.client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %v", token.Error())
	}

	return nil
}

func (m *MQTTClient) PublishDisconnectTractor() {
	if m.client != nil {
		topic := fmt.Sprintf("%s/disconnect-tractor", baseTopic)
		payload := "robot"
		token := m.client.Publish(topic, 0, false, []byte(payload))
		if token.Wait() && token.Error() != nil {
			log.Printf("Failed to publish disconnect-tractor: %v", token.Error())
		} else {
			log.Printf("Published disconnect-tractor message to %s", topic)
		}
	}
}

func (m *MQTTClient) Disconnect() {
	if m.client != nil {
		// Publish disconnect-tractor before disconnecting
		m.PublishDisconnectTractor()

		m.client.Disconnect(250)
		log.Println("Disconnected from MQTT broker")
	}
}
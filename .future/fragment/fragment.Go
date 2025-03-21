package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math/rand"
	"net"
	"sync"
	"time"
)

const (
	// MaxUDPSize est la taille maximale standard d'un paquet UDP (IPv4).
	MaxUDPSize = 65507
	// FragmentHeaderSize est la taille en octets de l'en-tête de fragmentation.
	FragmentHeaderSize = 10 // ID (4) + Fragment Number (2) + Total Fragments (2) + Flags (2)
	// MaxFragmentSize est la taille maximale des données utiles dans un fragment.
	MaxFragmentSize = 1400 - FragmentHeaderSize // MTU typique Ethernet - En-tête UDP - En-tête IP - En-tête Fragmentation
	// ReassemblyTimeout est le temps après lequel on abandonne le réassemblage d'un message.
	ReassemblyTimeout = 5 * time.Second
)

// FragmentHeader représente l'en-tête ajouté à chaque fragment UDP.
type FragmentHeader struct {
	MessageID      uint32 // Identifiant unique du message fragmenté
	FragmentNumber uint16 // Numéro du fragment (commence à 0)
	TotalFragments uint16 // Nombre total de fragments pour ce message
	Flags          uint16 // Flags (ex: dernier fragment)
}

const (
	FlagLastFragment uint16 = 1 << 0 // Bit de flag pour indiquer le dernier fragment
)

// fragmentedMessage représente un message en cours de réassemblage.
type fragmentedMessage struct {
	fragmentsReceived [][]byte
	lastFragmentTime  time.Time
	totalFragments    uint16
}

var (
	reassemblyBuffers      = make(map[uint32]*fragmentedMessage)
	reassemblyBuffersMutex sync.Mutex
)

// generateMessageID génère un ID de message unique aléatoire.
func generateMessageID() uint32 {
	return rand.Uint32()
}

// FragmentData divise les données en fragments UDP.
func FragmentData(data []byte) ([][]byte, error) {
	messageID := generateMessageID()
	numFragments := (len(data) + MaxFragmentSize - 1) / MaxFragmentSize
	if numFragments > 65535 { // Limite pratique pour TotalFragments (uint16)
		return nil, errors.New("message trop grand pour être fragmenté en moins de 65536 fragments")
	}
	fragments := make([][]byte, 0, numFragments)

	for i := 0; i < numFragments; i++ {
		start := i * MaxFragmentSize
		end := start + MaxFragmentSize
		if end > len(data) {
			end = len(data)
		}
		fragmentData := data[start:end]

		header := FragmentHeader{
			MessageID:      messageID,
			FragmentNumber: uint16(i),
			TotalFragments: uint16(numFragments),
		}
		if i == numFragments-1 {
			header.Flags |= FlagLastFragment // Marquer le dernier fragment
		}

		headerBuf := new(bytes.Buffer)
		if err := binary.Write(headerBuf, binary.BigEndian, header); err != nil {
			return nil, fmt.Errorf("binary.Write header failed: %w", err)
		}

		packet := append(headerBuf.Bytes(), fragmentData...)
		fragments = append(fragments, packet)
	}

	return fragments, nil
}

// ReassembleData réassemble les fragments UDP en données originales.
func ReassembleData(packet []byte) ([]byte, error) {
	if len(packet) < FragmentHeaderSize {
		return nil, errors.New("paquet trop petit pour contenir un en-tête de fragment")
	}

	headerBuf := bytes.NewReader(packet[:FragmentHeaderSize])
	var header FragmentHeader
	if err := binary.Read(headerBuf, binary.BigEndian, &header); err != nil {
		return nil, fmt.Errorf("binary.Read header failed: %w", err)
	}
	fragmentPayload := packet[FragmentHeaderSize:]

	reassemblyBuffersMutex.Lock()
	defer reassemblyBuffersMutex.Unlock()

	msg, ok := reassemblyBuffers[header.MessageID]
	if !ok {
		msg = &fragmentedMessage{
			fragmentsReceived: make([][]byte, header.TotalFragments),
			lastFragmentTime:  time.Now(),
			totalFragments:    header.TotalFragments,
		}
		reassemblyBuffers[header.MessageID] = msg
	}

	if int(header.FragmentNumber) >= len(msg.fragmentsReceived) {
		return nil, fmt.Errorf("numéro de fragment invalide: %d, total attendu: %d", header.FragmentNumber, len(msg.fragmentsReceived))
	}
	msg.fragmentsReceived[header.FragmentNumber] = fragmentPayload
	msg.lastFragmentTime = time.Now()

	// Vérifier si tous les fragments sont arrivés et si le timeout n'est pas dépassé
	if isComplete(msg) {
		reassembledData, err := assembleMessage(header.MessageID)
		if err != nil {
			return nil, err
		}
		delete(reassemblyBuffers, header.MessageID) // Nettoyer après réassemblage
		return reassembledData, nil
	}

	return nil, nil // Réassemblage incomplet
}

// isComplete vérifie si tous les fragments d'un message sont reçus.
func isComplete(msg *fragmentedMessage) bool {
	if time.Since(msg.lastFragmentTime) > ReassemblyTimeout {
		return true // Considérer comme complet (timeout) pour le nettoyage, même si incomplet
	}
	for _, frag := range msg.fragmentsReceived {
		if frag == nil {
			return false // Fragment manquant
		}
	}
	return true // Tous les fragments reçus
}

// assembleMessage assemble les fragments en un message complet.
func assembleMessage(messageID uint32) ([]byte, error) {
	reassemblyBuffersMutex.Lock()
	msg, ok := reassemblyBuffers[messageID]
	delete(reassemblyBuffers, messageID) // Supprimer immédiatement pour éviter double assemblage si appelé plusieurs fois par erreur
	reassemblyBuffersMutex.Unlock()

	if !ok {
		return nil, errors.New("message ID non trouvé dans les buffers de réassemblage")
	}

	if !isComplete(msg) && time.Since(msg.lastFragmentTime) <= ReassemblyTimeout {
		return nil, errors.New("réassemblage incomplet, timeout non atteint (devrait pas arriver ici si isComplete est bien implémenté)")
	}

	var fullData bytes.Buffer
	for _, frag := range msg.fragmentsReceived {
		if frag != nil { // S'assurer de ne pas écrire de fragments nil (cas de timeout/perte)
			fullData.Write(frag)
		}
	}

	if fullData.Len() == 0 && msg.totalFragments > 0 { // Cas où on attendait des fragments mais rien reçu (timeout total)
		return nil, errors.New("timeout de réassemblage, message incomplet ou perdu")
	}

	return fullData.Bytes(), nil
}

// handleUDPConn gère la connexion UDP et le réassemblage.
func handleUDPConn(conn *net.UDPConn) {
	defer conn.Close()
	buffer := make([]byte, MaxUDPSize)

	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Erreur de lecture UDP:", err)
			return
		}

		reassembledData, err := ReassembleData(buffer[:n])
		if err != nil {
			fmt.Println("Erreur de réassemblage:", err)
			continue // Continuer à écouter même en cas d'erreur de réassemblage
		}

		if reassembledData != nil {
			// Message réassemblé avec succès
			checksum := binary.BigEndian.Uint32(reassembledData[:4]) // Supposons les 4 premiers octets comme checksum
			dataPayload := reassembledData[4:]

			calculatedChecksum := crc32.ChecksumIEEE(dataPayload)
			if calculatedChecksum != checksum {
				fmt.Printf("Checksum invalide reçu de %s, données corrompues!\n", addr.String())
				continue
			}

			fmt.Printf("Message réassemblé reçu de %s: %s\n", addr.String(), string(dataPayload))
		} else {
			// Réassemblage incomplet, attendre d'autres fragments
			fmt.Printf("Fragment reçu de %s, réassemblage en cours...\n", addr.String())
		}
	}
}

func main() {
	// --- Partie Serveur (Réception et Réassemblage) ---
	udpAddr, err := net.ResolveUDPAddr("udp", ":20001")
	if err != nil {
		panic(err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	fmt.Println("Serveur UDP en écoute sur", udpAddr)
	go handleUDPConn(conn)

	// --- Partie Client (Fragmentation et Envoi) ---
	clientAddr, err := net.ResolveUDPAddr("udp", "localhost:0") // Port aléatoire pour le client
	if err != nil {
		panic(err)
	}
	serverAddr, err := net.ResolveUDPAddr("udp", "localhost:20001")
	if err != nil {
		panic(err)
	}

	clientConn, err := net.DialUDP("udp", clientAddr, serverAddr)
	if err != nil {
		panic(err)
	}
	defer clientConn.Close()

	// Créer un message plus grand que MaxFragmentSize
	messagePayload := generateLargeMessage(2000) // Message de 2000 octets
	checksum := crc32.ChecksumIEEE(messagePayload)

	fullMessage := new(bytes.Buffer)
	binary.Write(fullMessage, binary.BigEndian, checksum) // Ajouter le checksum en préfixe
	fullMessage.Write(messagePayload)

	fragments, err := FragmentData(fullMessage.Bytes())
	if err != nil {
		panic(err)
	}

	fmt.Println("Envoi du message fragmenté...")
	for _, fragment := range fragments {
		_, err := clientConn.Write(fragment)
		if err != nil {
			fmt.Println("Erreur d'envoi du fragment:", err)
		}
		time.Sleep(10 * time.Millisecond) // Simuler un débit réseau et étaler l'envoi
	}
	fmt.Println("Message fragmenté envoyé.")

	// Garder le programme en cours d'exécution pour que le serveur continue d'écouter
	fmt.Println("Serveur en attente de messages... (Appuyez sur Ctrl+C pour arrêter)")
	select {} // Bloquer indéfiniment le main pour que le serveur continue de tourner
}

// generateLargeMessage crée un message de test de taille spécifiée.
func generateLargeMessage(size int) []byte {
	data := make([]byte, size)
	rand.Read(data) // Remplir avec des données aléatoires
	return data
}

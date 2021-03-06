package ztn

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/inverse-inc/packetfence/go/remoteclients"
	"github.com/inverse-inc/packetfence/go/sharedutils"
	"github.com/inverse-inc/wireguard-go/device"
	"github.com/jackpal/gateway"
)

type ServerChallenge struct {
	Challenge      string `json:"challenge"`
	PublicKey      string `json:"public_key"`
	BytesChallenge []byte
	BytesPublicKey [32]byte
}

func DoServerChallenge(profile *Profile) string {
	sc, err := GetServerChallenge(profile)
	sharedutils.CheckError(err)

	privateKey, err := remoteclients.B64KeyToBytes(profile.PrivateKey)
	sharedutils.CheckError(err)

	publicKey, err := remoteclients.B64KeyToBytes(profile.PublicKey)
	sharedutils.CheckError(err)

	challenge, err := sc.Decrypt(privateKey)
	sharedutils.CheckError(err)

	challenge = append(challenge, publicKey[:]...)

	challengeEncrypted, err := sc.Encrypt(privateKey, challenge)
	sharedutils.CheckError(err)

	return base64.URLEncoding.EncodeToString(challengeEncrypted)
}

func GetServerChallenge(profile *Profile) (ServerChallenge, error) {
	sc := ServerChallenge{}
	err := GetAPIClient().Call(APIClientCtx, "GET", "/api/v1/remote_clients/server_challenge?public_key="+url.QueryEscape(b64keyToURLb64(profile.PublicKey)), &sc)
	if err != nil {
		return sc, err
	}

	sc.BytesChallenge, err = base64.URLEncoding.DecodeString(sc.Challenge)
	if err != nil {
		return sc, err
	}

	sc.BytesPublicKey, err = remoteclients.URLB64KeyToBytes(sc.PublicKey)
	if err != nil {
		return sc, err
	}

	return sc, nil
}

func (sc *ServerChallenge) Decrypt(privateKey [32]byte) ([]byte, error) {
	sharedSecret := remoteclients.SharedSecret(privateKey, sc.BytesPublicKey)
	return remoteclients.DecryptMessage(sharedSecret[:], sc.BytesChallenge)
}

func (sc *ServerChallenge) Encrypt(privateKey [32]byte, message []byte) ([]byte, error) {
	sharedSecret := remoteclients.SharedSecret(privateKey, sc.BytesPublicKey)
	return remoteclients.EncryptMessage(sharedSecret[:], message)
}

type Profile struct {
	WireguardIP      net.IP   `json:"wireguard_ip"`
	WireguardNetmask int      `json:"wireguard_netmask"`
	PublicKey        string   `json:"public_key"`
	PrivateKey       string   `json:"private_key"`
	AllowedPeers     []string `json:"allowed_peers"`
	logger           *device.Logger
}

func (p *Profile) SetupWireguard(device *device.Device, WGInterface string) {
	p.setupInterface(device, WGInterface)

	SetConfig(device, "listen_port", fmt.Sprintf("%d", localWGPort))
	SetConfig(device, "private_key", keyToHex(p.PrivateKey))
	SetConfig(device, "persistent_keepalive_interval", "5")
}

func (p *Profile) FillProfileFromServer(logger *device.Logger) {
	p.logger = logger

	auth := DoServerChallenge(p)

	mac, err := p.findClientMAC()
	sharedutils.CheckError(err)

	err = GetAPIClient().Call(APIClientCtx, "GET", "/api/v1/remote_clients/profile?public_key="+url.QueryEscape(b64keyToURLb64(p.PublicKey))+"&auth="+url.QueryEscape(auth)+"&mac="+url.QueryEscape(mac.String()), &p)
	sharedutils.CheckError(err)
}

func (p *Profile) findClientMAC() (net.HardwareAddr, error) {
	gwIP, err := gateway.DiscoverGateway()
	sharedutils.CheckError(err)

	p.logger.Debug.Println("Found default gateway", gwIP)

	ifaces, err := net.Interfaces()
	sharedutils.CheckError(err)

	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			p.logger.Error.Printf("Unable to get IP address for interface: %v\n", err.Error())
			continue
		}
		for _, a := range addrs {
			switch ipnet := a.(type) {
			case *net.IPNet:
				if ipnet.Contains(gwIP) {
					p.logger.Info.Println("Found MAC address", i.HardwareAddr, "on interface", ipnet, "("+i.Name+")")
					return i.HardwareAddr, nil
				}
			}
		}
	}

	return net.HardwareAddr{}, errors.New("Unable to find MAC address")
}

type PeerProfile struct {
	WireguardIP net.IP `json:"wireguard_ip"`
	PublicKey   string `json:"public_key"`
}

func GetPeerProfile(id string) (PeerProfile, error) {
	var p PeerProfile
	err := GetAPIClient().Call(APIClientCtx, "GET", "/api/v1/remote_clients/peer/"+id, &p)

	pkey, err := base64.URLEncoding.DecodeString(p.PublicKey)
	sharedutils.CheckError(err)
	p.PublicKey = base64.StdEncoding.EncodeToString(pkey)

	return p, err
}

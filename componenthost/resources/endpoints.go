package resources

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"strings"
	"time"
)

const EndpointSchemaV1 = canonical.EndpointSchemaV1

type Network = canonical.Network

const NetworkTCP = canonical.NetworkTCP

const NetworkUDP = canonical.NetworkUDP

const NetworkUnknown = canonical.NetworkUnknown

type AddressFamily = canonical.AddressFamily

const AddressFamilyIPv4 = canonical.AddressFamilyIPv4

const AddressFamilyIPv6 = canonical.AddressFamilyIPv6

const AddressFamilyUnknown = canonical.AddressFamilyUnknown

type EndpointIntent = canonical.EndpointIntent

const EndpointIntentPublic = canonical.EndpointIntentPublic

const EndpointIntentPrivate = canonical.EndpointIntentPrivate

const EndpointIntentLocal = canonical.EndpointIntentLocal

const EndpointIntentUnknown = canonical.EndpointIntentUnknown

type PublicEndpointKey = canonical.PublicEndpointKey

type AdvertisedEndpoint = canonical.AdvertisedEndpoint

type PublicEndpoint = canonical.PublicEndpoint

func EndpointIntentForBind(value string) EndpointIntent {
	return canonical.EndpointIntentForBind(value)
}

func AddressFamilyForListen(value string) AddressFamily {
	return canonical.AddressFamilyForListen(value)
}

func NetworkForProtocol(value string) Network { return canonical.NetworkForProtocol(value) }

func BuildEndpointFact(resource ProtectableResource, network Network, now time.Time, reasons ...string) PublicEndpoint {
	return canonical.BuildEndpointFact(resource, network, now, reasons...)
}

func normalizeEndpointFact(resource ProtectableResource, value PublicEndpoint, now time.Time) PublicEndpoint {
	return canonical.NormalizeEndpointFact(resource, value, now)
}

func safeEndpointToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return ""
	}
	return value
}

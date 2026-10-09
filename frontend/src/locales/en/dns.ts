export default {
  "dns": {
    "capabilityUnavailable": "This transport is unavailable. Correct the configuration using the backend capability reason:",
    "mdnsSemantics": "mDNS uses multicast on eligible interfaces for .local and IPv4/IPv6 link-local reverse domains. It does not apply local resolver neighbor-domain or prefer-Go options.",
    "mdnsInterfaces": "Multicast interfaces (comma separated)",
    "mdnsInterfaceHint": "Leave empty for eligible interfaces. Each selected interface must be up, multicast capable and non-loopback with a usable address.",
    "mdnsObserved": "Currently observed eligible interfaces:",
    "resolvedSemantics": "The Linux service in this core owns the resolve1 D-Bus name. Another owner prevents use. Availability is a read-only observation; startup still checks permission and claims the name.",
    "resolvedMissingService": "Select an existing resolved service in this core. Create or correct the service before saving this DNS transport.",
    "add": "Add Dns Server",
    "empty": "No DNS servers configured",
    "title": "Dns Servers",
    "final": "Final",
    "server": "Server",
    "firstServer": "First Server",
    "cacheCapacity": "Cache Capacity",
    "disableCache": "Disable Cache",
    "disableExpire": "Disable Expire",
    "independentCache": "Independent Cache",
    "reverseMapping": "Reverse Mapping",
    "domainStrategy": "Domain Strategy",
    "local": {
      "preferGo": "Prefer Go"
    },
    "rule": {
      "add": "Add Dns Rule",
      "empty": "No DNS rules configured",
      "title": "Dns Rules",
      "inet4Range": "IPv4 Range",
      "inet6Range": "IPv6 Range",
      "acceptDefault": "Accept Default Resolvers",
      "queryType": "Query Type",
      "ipAcceptAny": "Accept Empty IP",
      "rulesetAcceptEmpty": "Ruleset IP CIDR Accept Empty",
      "action": {
        "title": "Action",
        "route": "Route",
        "routeOptions": "Route Options",
        "reject": "Reject",
        "predefined": "Predefined",
        "rewriteTtl": "Rewrite TTL",
        "clientSubnet": "Client Subnet",
        "rcode": "Response Code",
        "rcodes": {
          "noError": "Ok",
          "formerr": "Bad request",
          "servFail": "Server failure",
          "nxDomain": "Not found",
          "refused": "Refused",
          "notImp": "Not Implemented"
        },
        "answer": "Answers",
        "ns": "Nameservers",
        "extra": "Extra"
      }
    }
  }
}

# Local Host Network Identity

Authenticated `GET /api/v1/network-identity` exposes one bounded Local Host
snapshot. The existing host-network provider samples interfaces and addresses
once per read. Its legacy forwarded-ingress adapter shares that observation
seam and retains its exact scope/revision projection and stricter name rules.
Observation scope is the Panel process's current network namespace. In a
container this does not establish facts about the physical host namespace.

Interface addresses retain the assigned address and prefix length. Range
classification distinguishes private, shared CGNAT, loopback, link-local,
documentation, benchmark, multicast, special and global-unicast values. These
are address-range facts. Global-unicast is not reachability; private is not a
LAN role. Down/loopback/virtual interfaces remain visible. Virtual names are
explicitly hints. OS interface names, including Windows Unicode/space names,
do not establish a role or durable device identity.

LAN roles, WAN, route-selected source, external egress and forwarded-ingress
authority remain UNKNOWN with reasons because no such trusted provider is
shipped at this seam. VPS operation does not require ubus. A WAN-like interface
name or Docker private interface never proves host WAN. No external service,
route probe, background poll, OS command or network mutation is introduced.

Existing resource inventory supplies configured listen intents, configured
resource hostnames and explicit advertised-endpoint inputs. Each retains its
resource/owner/revision and CONFIGURED_INPUT authority. The snapshot neither
selects an advertised winner nor infers observed sockets, external reachability
or an Inbound endpoint from them. Browser Host/location and forwarded headers
are not evidence inputs. Explicit settings, legacy server ipv4/ipv6 arrays,
Routing, DNS and subscription publishing behavior are unchanged.

Interface evidence expires after one minute. Resource reads use the existing
five-second inventory cache window. Every collection and value carries source,
authority, revision, observation time, expiry and a reason. Partial observations
retain their known values while the collection reports UNKNOWN completeness.
Consumers of a stored snapshot use Finalize at read time to age all evidence;
stale values may remain diagnostic values with UNKNOWN evidence. No old value
becomes current because its enclosing snapshot was read again.

The shared Host reference admits LOCAL/local only. Remote Host registration,
Node transport and cross-repository contracts are outside this implementation.

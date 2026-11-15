# Headless Server Protection consumer

This independent test module consumes the same immutable feature and Host versions selected by the Product source lock. It imports no Product package and supplies no firewall, SSH, database or filesystem executor.

It exercises the actual published descriptor, feature-to-Host typed intent, input isolation, unknown capability denial, management guard and inactive observation behavior. The existing Node checkout and its module pins are unchanged. Production Node transport, lifecycle composition, durable adapters, trusted Broker and Network Planner integration require their own qualified embedding and are not claimed by this fixture.

The Product's `scripts/protection-headless-tests.mjs` verifies version/checksum equality before running these tests under race on Linux and Windows.

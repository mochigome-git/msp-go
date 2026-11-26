# mochigome-git:msp-go

Reads data from Mitsubishi PLCs over the MC protocol (SLMP) and publishes it
to an MQTT broker, or copies values directly between two PLCs.

- **Mitsubishi**: 3E frame binary (iQ-R / Q / L) or 1E frame (FX series, `PLC_MODEL=true`)
- **Shibaura** (experimental, read-only): Modbus TCP probe for injection molding machines
- **MQTT**: plain TCP or TLS (AWS IoT style client certificates), automatic reconnect
- **Block reads**: group nearby devices into batch reads to save PLC scan time
- **Direct mode** (`MQTT_SKIP=true`): copy values between the main and secondary PLC, optionally gated by interlock bits

## Quick start

```bash
cp .env.local.example .env.local   # then edit it
docker compose up -d
```

`docker-compose.yml`:

```yaml
services:
  msp:
    container_name: msp-go
    image: mochigome/msp-go:2.22.5
    restart: always
    stop_grace_period: 30s
    env_file:
      - .env.local
    # with MQTTS_ON=true and certificate files:
    # volumes:
    #   - ./certs:/app/certs:ro
```

The container runs as a non-root user (uid 65532); mounted certificate files
must be readable by it.

## Configuration

Every option is documented in [`.env.local.example`](.env.local.example). Any
value that cannot be parsed, any malformed `DEVICES_*` entry, and any
malformed write map **stops startup with an error** rather than being skipped.

### Devices

`DEVICES_*` is a comma-separated list of `TYPE,NUMBER,DATATYPE` triples:

| DATATYPE | Meaning |
|---|---|
| 1 | 16-bit unsigned |
| 2 | 32-bit float |
| 3 | bit |
| 4 | text (`TEXT_ENCODING=auto` ASCII/Shift-JIS, or `unicode`) |
| 5 | 16-bit signed |
| 6 | FX ×10 value |
| 7 | 32-bit signed |

`X`, `Y`, `B`, `W` numbers are hexadecimal (`X,3E,3`), all others decimal.
For a Shibaura PLC the third field is a register/bit **count** instead.

### MQTT

Each value is published as JSON `{"address":"D100","value":123,"source":"main"}`
to `<MQTT_TOPIC><address>`, or to `<MQTT_TOPIC><plc>/<address>` with
`MQTT_TOPIC_INCLUDE_SOURCE=true`.

| Option | Default | |
|---|---|---|
| `MQTT_CLIENT_ID` | `msp-go-<hostname>` | must be unique per broker |
| `POLL_INTERVAL_MS` | `0` | pause between read cycles; 0 = continuous |
| `PUBLISH_ON_CHANGE` | `false` | publish only changed values… |
| `PUBLISH_REFRESH_SEC` | `60` | …plus a full refresh this often |
| `MQTTS_ON` | `false` | TLS; certificates from `MQTT_CA_CERTIFICATE` / `MQTT_CLIENT_CERTIFICATE` / `MQTT_PRIVATE_KEY` files, or PEM contents in `ECS_MQTT_*` |

If the broker is down the service keeps reading and retries the connection;
messages are dropped (QoS 0) until it reconnects.

### Direct writes between PLCs (`MQTT_SKIP=true`)

```bash
# copy D100 (read from either PLC) into main D200, and X3E into main M70 as a bit
WRITE_MAP_SEC_TO_PRIM=D100>D,200,1,1;X3E>M,70,3
# only while M64 is 1
WRITE_MAP_SEC_TO_PRIM_CONDITION=M64==D100;M64==X3E
```

Entry format: `SRC>TYPE,NUMBER[,WRITETYPE[,REGS]]`. Condition format:
`BIT==SRC` (write while BIT is 1) or `BIT!=SRC` (write while BIT is not 1).
Once a PLC has a `_CONDITION`, sources without a rule are never written.
Condition values from a PLC that becomes unreachable, or older than 30 s,
are ignored. `WRITE_MAP_PRIM_TO_SEC` is the reverse direction.

## Development

```bash
go test -race ./...
```

Diagnostics: `PPROF_PORT=6060` serves pprof on `127.0.0.1` inside the
container; `PERF_MONITOR=true` prints message throughput every second.

## Links

- Source: [GitHub](https://github.com/mochigome-git/msp-go)
- Docker Hub: [mochigome/msp-go](https://hub.docker.com/r/mochigome/msp-go)

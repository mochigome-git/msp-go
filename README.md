# mochigome-git:msp-i

Overview:
This Docker image provides a solution for extracting data from Mitsubishi brand Programmable Logic Controllers (PLCs) using 1E Frame or 3C Frame communication protocols and publishing it via MQTT (Message Queuing Telemetry Transport).

Features:

PLC Connectivity: Supports communication with Mitsubishi brand PLCs using 1E Frame or 3C Frame protocols.
Data Extraction: Extracts data from PLCs for monitoring and control purposes.
MQTT Integration: Publishes the extracted data to MQTT brokers for real-time data streaming and analysis.
Scalability: Built on Docker, allowing easy deployment and scaling across different environments.
Flexibility: Supports customization and configuration to adapt to various PLC setups and MQTT configurations.
Security: Utilizes TLS/SSL for secure communication between components, ensuring data integrity and confidentiality.
Usage:

Docker Run Command: Run the Docker container with appropriate environment variables and settings for connecting to the Mitsubishi PLC and MQTT broker.
You can use this Docker Compose file to run the container with the specified environment variables. Make sure to replace ${MQTT_HOST}, ${MQTT_TOPIC}, ${PLC_HOST}, ${PLC_PORT}, ${DEVICES_2bit}, ${DEVICES_16bit}, and ${DEVICES_32bit} with the appropriate values for your environment.

docker-compose.yml:

```bash
version: '3'

services:
  nk3-msp-pub:
    container_name: nk3-msp-pub
    image: mochigome/nk3-msp:1.93v
    restart: always
    logging:
      driver: "json-file"
      options:
        max-size: "20m"
        max-file: "10"
    environment:
      MQTT_HOST: "${MQTT_HOST}"
      MQTT_TOPIC: "${MQTT_TOPIC}"
      PLC_HOST: "${PLC_HOST}"
      PLC_PORT: "${PLC_PORT}"
      DEVICES_2bit: "${DEVICES_2bit}"
      DEVICES_16bit: "${DEVICES_16bit}"
      DEVICES_32bit: "${DEVICES_32bit}"
```

.env :
```bash
############
# Secrets 
# YOU MUST CHANGE THESE BEFORE GOING INTO PRODUCTION
############

PLC_HOST=192.168.3.21
PLC_PORT=5011

############
# MQTT 
############

# MQTT over SSL use mqtts, MQTT non SSL use tcp
MQTT_HOST=tcp://$IP_ADDRESS:1883  
MQTT_TOPIC="test/topic/"

# "mean" to post the average of values 
# "first" to post the first element
KEY_OPTION=mean

############
# DEVICES NO
############

DEVICES_16bit=D,0,1,D,1,1,D,2,1,D,3,1
DEVICES_32bit=D,650,2,D,676,2,D,106,2
DEVICES_2bit=M,24,3,M,25,3,M,26,3
```

Example:

```bash
docker-compose up -d
```

Note: Ensure that appropriate network configurations and security measures are in place to protect sensitive data and maintain system integrity.

License: none

Source Code: [GitHub](https://github.com/mochigome-git/nk3-PLCcapture-go)

Docker Hub: [mochigome/msp-i](https://hub.docker.com/repository/docker/mochigome/msp-i/general)

Maintainer: [mochigome-git](https://github.com/mochigome-git)

# Harlot

https://github.com/samuelships/harlot/assets/51783214/1e5670db-0b49-4d36-988d-4916d9b25b9d

**Harlot** exposes any service that speaks TCP behind a NAT to the public internet

## Installation

Ensure Go (1.15 or later) is installed.

```
git clone https://github.com/yourusername/harlot.git
cd harlot
go build -o harlot_platform
```

## Usage

on the server
```
harlot_platform server start
```

on the client
```
harlot_platform client start --protocol http --port 8080 example
```

Note: Ensure serverKey.pem and serverCert.pem are available on both server and client.

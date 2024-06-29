# Harlot
https://github.com/samuelships/harlot/assets/51783214/aa3a0e8a-73b8-4e22-8f26-b159519d0d52

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

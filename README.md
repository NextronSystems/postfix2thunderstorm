# Milter 2 THOR-Thunderstorm - Postfix Milter service for scanning with THOR Thunderstorm

![image](https://github.com/NextronSystems/postfix2thunderstorm/assets/8741929/470abb8e-1b44-43df-8c84-dee3de92cf1b)

The Postfix mail server is a popular and highly configurable Mail Transfer Agent (MTA) used for routing and delivering email messages within a network or across the internet. Similar to the Sendmail MTA, it can use Milter (protocol) to scan incoming emails for spam or malware. On incoming emails, compatible MTAs use the Milter protocol to communicate with an extra service, which also speaks the Milter protocol. This extra service scans the email and responds with its findings. Based on the response of the extra service the MTA can filter, discard, or quarantine the email. `postfix2thunderstorm` is a free and open-source implementation of a Milter Service which allows you to scan emails using THOR Thunderstorm. Read more about this in the following [blog post](https://www.nextron-systems.com/2023/11/14/supercharged-postfix)

## Build

Requires Go >= 1.20

```bash
go build -o postfix2thunderstorm cmd/main/main.go
```

## Usage

```bash
./postfix2thunderstorm -h
```
```bash
-config string
       Config filepath (default "./p2t.config.yaml")
-debug
       Debug flag

```

## Running

```bash
./postfix2thunderstorm --config p2t.config.yaml
```

## Config

Below is an [example](https://github.com/NextronSystems/postfix2thunderstorm/blob/master/p2t.config.yaml) configuration that can be used with `postfix2thunderstorm`

```yml
log_filepath: ./postfix2thunderstorm.log                                        # log filepath
max_filesize_bytes: 50_000_000                                                  # max size in bytes
active_mode: true                                                               # if true mails are quarantied based on 'quarantine_expression', else its in 'passive-mode'
milter_host: localhost                                                          # host to listen on, postfix will connect here
milter_port: 11337                                                              # port to listen on
thorthunderstorm_url: http://localhost:8080/api/check                           # Thor Thunderstorm endpoint
quarantine_expression: one(Matches, {.Subscore > 90}) or FullMatch.Score > 90   # Expression (https://github.com/antonmedv/expr) used for deciding if email should be quarantined
# Objects (e.g., '.Subscore' and FullMatch) to work with can be found in "milter.go:16"
```

There is an automatic log file rotation (~ 3 months of logs): 

* MaxSize:    500 megabytes
* MaxBackups: 3
* MaxAge:     31 days

It might be a good idea to monitor the log file for level `warning` and `error` messages.

Notably you want to look for `warning` level lines with the following message:

* `msg:"Finding"` --> THOR Thunderstorm found something suspicious
* `msg:"Quarantined email"` --> THOR Thunderstorm found something and the `quarantine_expression` triggered.

Postfix will place quarantined mails into its "hold" queue where they can be inspected and released or deleted.

## Postfix

Tested with version 3.6.4 - but should work with any recent version.

### Postfix Config

Add the follwoing to your Postfix config (/etc/postfix/main.cf) and restart it:
```
# See https://www.postfix.org/MILTER_README.html for more information
smtpd_milters = inet:<IP>:<Port> # IP/Port of host where the postfix2thunderstorm service is running (might be a good idea to make it the localhost (or use TLS))
milter_default_action = accept   # default action in case of error/timeout/...
```

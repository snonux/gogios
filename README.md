# Gogios

![Gogios](gogios-small.png "Gogios")

Gogios is a lightweight and minimalistic monitoring tool not designed for large-scale monitoring. It is ideal for monitoring self-hosted servers on a tiny scale, such as only a handful of servers or virtual machines (e.g. my personal infrastructure). If you have limited resources to monitor and require a simple yet effective solution, Gogios is an excellent choice. However, for larger environments with more complex monitoring requirements, it might be necessary to consider other monitoring solutions better suited for managing and scaling with increased monitoring demands.

You can also read about it in this blog post: https://foo.zone/gemfeed/2023-06-01-kiss-server-monitoring-with-gogios.html

## Example alert

This is an example alert report received via E-Mail. Whereas, `[C:2 W:0 U:0 S:0 OK:51]` means that we've got two alerts in status critical, 0 warnings, 0 unknowns, 0 stale alerts (last check too far in the past) and 51 OKs.

```
Subject: GOGIOS Report [C:2 W:0 U:0 S:0 OK:51]

This is the recent Gogios report!

# Alerts with status changed:

OK->CRITICAL: Check ICMP4 vulcan.buetow.org: Check command timed out
OK->CRITICAL: Check ICMP6 vulcan.buetow.org: Check command timed out

# Unhandled alerts:

CRITICAL: Check ICMP4 vulcan.buetow.org: Check command timed out
CRITICAL: Check ICMP6 vulcan.buetow.org: Check command timed out

# Stale alerts:

There are no stale alerts...

Have a nice day!
```

## Installation

### Compiling and installing Gogios

This README is primarily written for OpenBSD, but applying the corresponding steps to any Unix-like (e.g. Linux-based) operating system should be easy. On systems other than OpenBSD, you may always have to replace `does` with the `sudo` command and replace the `/usr/local/bin` path with `/usr/bin`.

To compile and install Gogios on OpenBSD, follow these steps:

```
git clone https://codeberg.org/snonux/gogios.git
cd gogios
go build -o gogios cmd/gogios/main.go # Or, alternatively: `task build` with taskfile.dev 
doas cp gogios /usr/local/bin/gogios
doas chmod 755 /usr/local/bin/gogios
```

You can use cross-compilation if you want to compile Gogios for OpenBSD on a Linux system without installing the Go compiler on OpenBSD. Follow these steps:

```
export GOOS=openbsd
export GOARCH=amd64
go build -o gogios cmd/gogios/main.go
```

On your OpenBSD system, copy the binary to `/usr/local/bin` and set the correct permissions as described in the previous section. All steps described here you could automate with your configuration management system of choice. I use Rexify, the friendly configuration management system, to automate the installation, but that is out of the scope of this document.

### Setting up user, group and directories

It is best to create a dedicated system user and group for Gogios to ensure proper isolation and security. Here are the steps to create the `_gogios` user and group under OpenBSD:

```
doas adduser -group _gogios -batch _gogios
doas usermod -d /var/run/gogios _gogios
doas mkdir -p /var/run/gogios
doas chown _gogios:_gogios /var/run/gogios
doas chmod 750 /var/run/gogios
echo if [ ! -d /var/run/gogios ]; then mkdir -p /var/run/gogios; fi | doas tee -a /etc/rc.local
echo chown _gogios:_gogios /var/run/gogios | doas tee -a /etc/rc.local
echo chmod 750 /var/run/gogios | doas tee -a /etc/rc.local
```

Please note that creating a user and group might differ depending on your operating system. For other operating systems, consult their documentation for creating system users and groups.

### Installing monitoring plugins

Gogios relies on external Nagios or Icinga monitoring plugin scripts. On OpenBSD, you can install the `monitoring-plugins` package with Gogios. The monitoring-plugins package is a collection of monitoring plugins, similar to Nagios plugins, that can be used to monitor various services and resources:

```
doas pkg_add monitoring-plugins
doas pkg_add nrpe # If you want to execute checks remotely via NRPE.
```

Once the installation is complete, you can find the monitoring plugins in the `/usr/local/libexec/nagios` directory, which then can be configured to be used in `gogios.json`.

## Configuration

### MTA

Gogios requires a local Mail Transfer Agent (MTA) such as Postfix or OpenBSD SMTPD running on the same server where the CRON job (see about the CRON job further below) is executed. The local MTA handles email delivery, allowing Gogios to send email notifications to monitor status changes. Before using Gogios, ensure that you have a properly configured MTA installed and running on your server to facilitate the sending of emails. Once the MTA is set up and functioning correctly, Gogios can leverage it to send email notifications.

You can use the mail command to send an email via the command line on OpenBSD. Here's an example of how to send a test email to ensure that your email server is working correctly:

```
echo 'This is a test email from OpenBSD.' | mail -s 'Test Email' your-email@example.com
```

Check the recipient's inbox to confirm the delivery of the test email. If the email is delivered successfully, it indicates that your email server is configured correctly and functioning. Please check your MTA logs in case of issues.

### Configuring Gogios

To configure Gogios, create a JSON configuration file (e.g., `/etc/gogios.json`). Here's an example configuration:

```
{
  "EmailTo": "paul@dev.buetow.org",
  "EmailFrom": "gogios@buetow.org",
  "CheckTimeoutS": 10,
  "CheckConcurrency": 2,
  "StaleThreshold": 3600,
  "StateDir": "/var/run/gogios",
  "Checks": {
    "Check ICMP4 www.foo.zone": {
      "Plugin": "/usr/local/libexec/nagios/check_ping",
      "Args": [ "-H", "www.foo.zone", "-4", "-w", "50,10%", "-c", "100,15%" ],
      "Retries": 3,
      "RetryInterval": 10
    },
    "Check ICMP6 www.foo.zone": {
      "Plugin": "/usr/local/libexec/nagios/check_ping",
      "Args": [ "-H", "www.foo.zone", "-6", "-w", "50,10%", "-c", "100,15%" ]
      "Retries": 3,
      "RetryInterval": 10
    },
    "www.foo.zone HTTP IPv4": {
      "Plugin": "/usr/local/libexec/nagios/check_http",
      "Args": ["www.foo.zone", "-4"],
      "DependsOn": ["Check ICMP4 www.foo.zone"]
    },
    "www.foo.zone HTTP IPv6": {
      "Plugin": "/usr/local/libexec/nagios/check_http",
      "Args": ["www.foo.zone", "-6"],
      "DependsOn": ["Check ICMP6 www.foo.zone"]
    }
    "Check NRPE Disk Usage foo.zone": {
      "Plugin": "/usr/local/libexec/nagios/check_nrpe",
      "Args": ["-H", "foo.zone", "-c", "check_disk", "-p", "5666", "-4"]
    }
  }
}
```

* `EmailTo`: Specifies the recipient of the email notifications.
* `EmailFrom`: Indicates the sender's email address for email notifications.
* `CheckTimeoutS`: Sets the timeout for checks in seconds.
* `CheckConcurrency`: Determines the number of concurrent checks that can run simultaneously.
* `StaleThreshold`: Defines the threshold in seconds for considering a check stale if it hasn't been updated within this time frame.
* `StateDir`: Specifies the directory where Gogios stores its persistent state in a `state.json` file. 
* `Checks`: Defines a list of checks to be performed, each with a unique name, plugin path, and arguments. 

Adjust the configuration file according to your needs, specifying the checks you want Gogios to perform.

If you want to execute checks only when another check succeeded (status OK), use `DependsOn`. In the example above, the HTTP checks won't run when the hosts aren't pingable. They will show up as `UNKNOWN` in the report.

`Retries` and `RetryInterval` are optional check configuration parameters. In case of failure, Gogios will retry `Retries` times each `RetryInterval` seconds.

For remote checks, use the `check_nrpe` plugin. You also need to have the NRPE server set up correctly on the target host (out of scope for this document).

The `state.json` file mentioned above keeps track of the monitoring state and check results between Gogios runs, enabling Gogios only to send email notifications when there are changes in the check status.

## Running Gogios

Now it is time to give it a first run. On OpenBSD, do:

```
doas -u _gogios /usr/local/bin/gogios -cfg /etc/gogios.json
```

To run Gogios via CRON on OpenBSD as the `gogios` user and check all services once per minute, follow these steps:

Type `doas crontab -e -u _gogios` and press Enter to open the crontab file for the `_gogios` user for editing and add the following lines to the crontab file:

```
*/5 8-22 * * * -s /usr/local/bin/gogios -cfg /etc/gogios.json
0 7 * * * /usr/local/bin/gogios -renotify -cfg /etc/gogios.json
0 3 * * 0 /usr/local/bin/gogios -force -cfg /etc/gogios.json
```

Gogios is now configured to run every five minutes from 8 AM to 10 PM via CRON as the `_gogios` user. It will execute checks and send monitoring status updates via email whenever a check status changes according to your configuration. Additionally, Gogios will run once at 7 AM every morning to re-notify all unhandled alerts as a reminder. Furthermore, Gogios will also run every Sunday morning at 3 AM and will send out a notification even if all checks are in the state OK, providing ensurance that the email server is still functional.

Notice the `-s` in the first CRON tab entry. This is incredibly useful for cron jobs that shouldn't run twice in parallel. If the job duration is longer than usual, you are ensured that it will never start a new instance until the previous one is done. This feature exists only in OpenBSD's CRON, so don't use it if you are using another OS.

### High-availability

To create a high-availability Gogios setup, you can install Gogios on two servers that will monitor each other using the NRPE (Nagios Remote Plugin Executor) plugin. By running Gogios in alternate CRON intervals on both servers, you can ensure that even if one server goes down, the other will continue monitoring your infrastructure and sending notifications.

* Install Gogios on both servers following the compilation and installation instructions provided earlier.
* Install the NRPE server (out of scope for this document) and plugin on both servers. This plugin allows you to execute Nagios check scripts on remote hosts.
* Configure Gogios on both servers to monitor each other using the NRPE plugin. Add a check to the Gogios configuration file (`/etc/gogios.json`) on both servers that uses the NRPE plugin to execute a check script on the other server. For example, if you have Server A and Server B, the configuration on Server A should include a check for Server B, and vice versa.
* Set up alternate CRON intervals on both servers. Configure the CRON job on Server A to run Gogios at minutes 0, 10, 20, ..., and on Server B to run at minutes 5, 15, 25, ... This will ensure that if one server goes down, the other server will continue monitoring and sending notifications. 
* Gogios doesn't support clustering. So it means when both servers are up, unhandled alerts will be notified via E-Mail twice; from each server once. That's the trade-off for simplicity. 
* There are plans to make it possible to execute certain checks only on certain nodes (e.g. on elected leader or master nodes). This is still in progress (check out my `Gorum` git project).

# But why?

With experience in monitoring solutions like Nagios, Icinga, Prometheus and OpsGenie, I know that these tools often came with many features that I didn't necessarily need for personal use. Contact groups, host groups, check clustering, and the requirement of operating a DBMS and a WebUI added complexity and bloat to my monitoring setup.

My primary goal was to have a single email address for notifications and a simple mechanism to periodically execute standard Nagios check scripts and notify me of any state changes. I wanted the most minimalistic monitoring solution possible but wasn't satisfied with the available options.

This led me to create Gogios, a lightweight monitoring tool tailored to my specific needs. I chose the Go programming language for this project as it allowed me to refresh my Go programming skills and provided a robust platform for developing a fast and efficient monitoring tool.

Gogios eliminates unnecessary features and focuses on simplicity, providing a no-frills monitoring solution for small-scale self-hosted servers and virtual machines. The result is a tool that is easy to configure, set up, and maintain, ensuring that monitoring your resources is as hassle-free as possible.

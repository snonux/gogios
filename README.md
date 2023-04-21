# Gogios

Gogios is a minimalistic and easy-to-use monitoring tool written in Go, compatible with the Nagios Check Plugins. It is designed to periodically execute checks and send monitoring status via email. With its simple configuration, Gogios is a perfect solution for those looking for a lightweight monitoring solution that integrates well with the Nagios ecosystem.

Gogios is a lightweight and minimalistic monitoring tool that is not designed for large-scale monitoring. It is ideal for monitoring self-hosted servers in a very small scale, such as only a handful of servers and/or virtual machines. If you have a limited number of resources to monitor and require a simple yet effective solution, Gogios is a great choice. However, for larger environments with more complex monitoring requirements, it might be necessary to consider other monitoring solutions that are better suited for managing and scaling with increased monitoring demands.

## Installation

### Compiling and installing Gogios

To compile and install Gogios on OpenBSD, follow these steps:

```
git clone https://codeberg.org/snonux/gogios.git
cd gogios
go build -o gogios cmd/gogios/main.go
doas cp gogios /usr/local/bin/gogios
doas chmod 755 /usr/local/bin/gogios
```
This README is primarily written for OpenBSD, but it should be easy to apply the corresponding steps to any other Unix like (e.g. Linux based ones) operating sytems. On systems other than OpenBSD you may always have to replace `doas` with the `sudo` command and replace the `/usr/local/bin` path with `/usr/bin`.

If you want to compile Gogios for OpenBSD on a Linux system without installing the Go compiler on OpenBSD, you can use cross-compilation. Follow these steps:

```
export GOOS=openbsd
export GOARCH=amd64
go build -o gogios cmd/gogios/main.go
```

On your OpenBSD system, copy the binary to `/usr/local/bin` and set the correct permissions as described in the previous section. All steps described here you could automate with your configuration management system of choice. I personally use Rexify, the friendly configuration management system, to automate the installation.

### Setting up user, group and directories

It is best to create a dedicated system user and group for Gogios to ensure proper isolation and security. The process of creating a user and group may vary depending on the operating system you're using. Here are the steps to create the `_gogios` user and group under OpenBSD:

```
doas adduser -group _gogios -batch _gogios
doas usermod -d /var/run/gogios _gogios
doas mkdir -p /var/run/gogios
doas chown _gogios:_gogios /var/run/gogios
doas chmod 750 /var/run/gogios
```

Please note that the process of creating a user and group might differ depending on the operating system you are using. For other operating systems, consult their documentation for creating system users and groups.

### Installing monitoring plugins

Gogios relies on external Nagios or Icinga monitoring plugin scripts. On OpenBSD, you can install the `monitoring-plugins` package to use with Gogios. The monitoring-plugins package is a collection of monitoring plugins, similar to Nagios plugins, that can be used to monitor various services and resources:

```
doas pkg_add monitoring-plugins
```

Once the installation is complete, you can find the monitoring plugins in the `/usr/local/libexec/nagios` directory, which then can be configured to be used in `gogios.json`.

## Configuration

### MTA

Gogios requires a local Mail Transfer Agent (MTA) such as Postfix or OpenBSD SMTPD running on the same server where the CRON job (see about the CRON job further below) is executed. The local MTA is responsible for handling email delivery, allowing Gogios to send out email notifications for monitoring status changes. Before using Gogios, ensure that you have a properly configured MTA installed and running on your server to facilitate the sending of emails. Once the MTA is set up and functioning correctly, Gogios can leverage it to send email notifications as needed.

To send an email via the command line on OpenBSD, you can use the mail command. Here's an example of how to send a test email to ensure that your email server is working correctly:

```
echo 'This is a test email from OpenBSD.' | mail -s 'Test Email' your-email@example.com
```

Check the recipient's inbox to confirm the delivery of the test email. If the email is delivered successfully, it indicates that your email server is properly configured and functioning. Please check your MTA logs in case of issues.

### Configuring Gogios

To configure Gogios, create a JSON configuration file (e.g., `/etc/gogios.json`). Here's a sample configuration:

```
{
  "EmailTo": "paul@dev.buetow.org",
  "EmailFrom": "gogios@buetow.org",
  "CheckTimeoutS": 10,
  "CheckConcurrency": 2,
  "StateDir": "/var/run/gogios",
  "Checks": {
    "www.foo.zone HTTP IPv4": {
      "Plugin": "/usr/local/libexec/nagios/check_http",
      "Args": ["www.foo.zone", "-4"]
    },
    "www.foo.zone HTTP IPv6": {
      "Plugin": "/usr/local/libexec/nagios/check_http",
      "Args": ["www.foo.zone", "-6"]
    }
  }
}
```

* `EmailTo`: Specifies the recipient of the email notifications.
* `EmailFrom`: Indicates the sender's email address for email notifications.
* `CheckTimeoutS`: Sets the timeout for checks in seconds.
* `CheckConcurrency`: Determines the number of concurrent checks that can run simultaneously.
* `StateDir`: Specifies the directory where Gogios stores its persistent state in a `state.json` file. 
* `Checks`: Defines a list of checks to be performed, with each check having a unique name, plugin path, and arguments.

Adjust the configuration file according to your needs, specifying the checks you want Gogios to perform.

The `state.json` file mentioned above keeps track of the monitoring state and check results between Gogios runs, enabling Gogios to only send email notifications when there are changes in the check status.

## Running Gogios

Now it is time to give it a first run. On OpenBSD, do:

```
doas -u _gogios /usr/local/bin/gogios -cfg /etc/gogios.json
```

To run Gogios via CRON on OpenBSD as the `gogios` user and check all services once per minute, follow these steps:

Type `doas crontab -e -u _gogios` and press Enter to open the crontab file for the `_gogios` user for editing and add the following lines to the crontab file:

```
*/5 8-22 * * * /usr/local/bin/gogios -cfg /etc/gogios.json
0 7 * * * /usr/local/bin/gogios -renotify -cfg /etc/gogios.json
```

Gogios is now configured to run every five minutes from 8am to 10pm via CRON as the `_gogios` user. It will execute the checks and send monitoring status whenever a check status changes via email according to your configuration. Also, Gogios will run once 7am every morning and re-notify all unhandled alerts as a reminder.

### High-availability

To create a high-availability Gogios setup, you can install Gogios on two servers that will monitor each other using the NRPE (Nagios Remote Plugin Executor) plugin. By running Gogios in alternate cron intervals on both servers, you can ensure that even if one server goes down, the other will continue monitoring your infrastructure and sending notifications.

* Install Gogios on both servers following the compilation and installation instructions provided earlier.
* Install the NRPE server and plugin on both servers. This plugin allows you to execute Nagios check scripts on remote hosts.
* Configure Gogios on both servers to monitor each other using the NRPE plugin. Add a check to the Gogios configuration file (`/etc/gogios.json`) on both servers that uses the NRPE plugin to execute a check script on the other server. For example, if you have Server A and Server B, the configuration on Server A should include a check for Server B, and vice versa.
* Set up alternate cron intervals on both servers. Configure the cron job on Server A to run Gogios at minutes 0, 10, 20, ..., and on Server B to run at minutes 5, 15, 25, ... This will ensure that if one server goes down, the other server will continue monitoring and sending notifications.

# But why?

As a Site Reliability Engineer with experience in various monitoring solutions like Nagios, Icinga, Prometheus and OpsGenie, I found that these tools often came with a plethora of features that I didn't necessarily need. Contact groups, host groups, re-check intervals, check clustering, and the requirement of operating a DBMS and a WebUI added complexity and bloat to my monitoring setup.

My primary goal was to have a single email address for notifications and a simple mechanism to periodically execute standard Nagios check scripts and notify me of any state changes. I wanted the most minimalistic monitoring solution possible, but I wasn't satisfied with the available options.

This led me to create Gogios, a lightweight monitoring tool tailored to my specific needs. I chose the Go programming language for this project as it not only allowed me to refresh my Go programming skills but also provided a robust platform for developing a fast and efficient monitoring tool.

Gogios eliminates unnecessary features and focuses on simplicity, providing a no-frills monitoring solution for small-scale self-hosted servers and virtual machines. The result is a tool that is easy to configure, set up, and maintain, ensuring that monitoring your resources is as hassle-free as possible.

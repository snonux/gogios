# Gogios

Gogios is a minimalistic and easy-to-use monitoring tool written in Golang, compatible with the Nagios Check API. It is designed to periodically execute checks and send monitoring status via email. With its simple configuration, Gogios is a perfect solution for those looking for a lightweight monitoring solution that integrates well with the Nagios ecosystem.

Gogios is a lightweight and minimalistic monitoring tool that is not designed for large-scale monitoring. It is ideal for monitoring self-hosted servers in a very small scale, such as only a handful of servers and/or virtual machines. If you have a limited number of resources to monitor and require a simple yet effective solution, Gogios is a great choice. However, for larger environments with more complex monitoring requirements, it might be necessary to consider other monitoring solutions that are better suited for managing and scaling with increased monitoring demands.

## Installation

To compile and install Gogios on OpenBSD, follow these steps:

```
git clone https://codeberg.org/snonux/gogios.git
cd gogios
go build -o gogios cmd/gogios/main.go
doas cp gogios /usr/local/bin/gogios
doas chmod 755 /usr/local/bin/gogios
```

Please note, depending on your operating system (e.g. Linux based), you may want to use `sudo` instead of `doas` and also change `/usr/local/bin` to a different path. If you want to compile Gogios for OpenBSD on a Linux system without installing the Go compiler on OpenBSD, you can use cross-compilation. Follow these steps:

```
export GOOS=openbsd
export GOARCH=amd64
go build -o gogios cmd/gogios/main.go
```

On your OpenBSD system, copy the binary to `/usr/local/bin` and set the correct permissions as described in the previous section. I personally use Rexify, the friendly configuration management system, to automate the installation.

## Configuration

### MTA

Gogios requires a local Mail Transfer Agent (MTA) such as Postfix or OpenBSD SMTPD running on the same server where the CRON job (see about the CRON job further below) is executed. The local MTA is responsible for handling email delivery, allowing Gogios to send out email notifications for monitoring status changes. Before using Gogios, ensure that you have a properly configured MTA installed and running on your server to facilitate the sending of emails. Once the MTA is set up and functioning correctly, Gogios can leverage it to send email notifications as needed.

To send an email via the command line on OpenBSD, you can use the mail command. Here's an example of how to send a test email to ensure that your email server is working correctly:

```
echo "This is a test email from OpenBSD." | mail -s "Test Email" your-email@example.com
```

Check the recipient's inbox to confirm the delivery of the test email. If the email is delivered successfully, it indicates that your email server is properly configured and functioning. Please check your MTA logs in case of issues.

### Gogios config

To configure Gogios, create a JSON configuration file (e.g., /etc/gogios.json). Here's a sample configuration:

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

## Setting up user, group and directories

It is best to create a dedicated system user and group for Gogios to ensure proper isolation and security. The process of creating a user and group may vary depending on the operating system you're using. Here are the steps to create the `gogios` user and group under OpenBSD:

```
sudo groupadd gogios
sudo useradd -g gogios -d /nonexistent -s /sbin/nologin -r gogios
```

Please note that the process of creating a user and group might differ depending on the operating system you are using. For other operating systems, consult their documentation for creating system users and groups.

To set up the `StateDir` correctly with the correct permissions, follow these steps: 

```
sudo mkdir -p /var/run/gogios
sudo chown gogios:gogios /var/run/gogios
sudo chmod 750 /var/run/gogios
```

## Running Gogios via CRON

Now it is time to give it a first run. On OpenBSD, do:

```
doas -u gogios /usr/local/bin/gogios -cfg /etc/gogios.json
```

and on Linux based systems, you likely would need to run:

```
sudo -u gogios /usr/local/bin/gogios -cfg /etc/gogios.json
```

To run Gogios via CRON as the `gogios` user and check all services once per minute, follow these steps:

Type `sudo crontab -e -u gogios` and press Enter to open the crontab file for the `gogios` user for editing and add the following line to the crontab file:

```
* * * * * /usr/local/bin/gogios -cfg /etc/gogios.json
```

Replace `/usr/local/bin/gogios` with the actual path to the Gogios binary on your system. Gogios is now configured to run every minute via CRON as the `gogios` user, and it will execute the checks and send monitoring status via email according to your configuration. By running Gogios under the gogios user's crontab, you further enhance the isolation and security of the monitoring setup.

## High-availability

To create a high-availability Gogios setup, you can install Gogios on two servers that will monitor each other using the NRPE (Nagios Remote Plugin Executor) plugin. By running Gogios in alternate cron intervals on both servers, you can ensure that even if one server goes down, the other will continue monitoring your infrastructure and sending notifications.

* Install Gogios on both servers following the compilation and installation instructions provided earlier.
* Install the NRPE server and plugin on both servers. This plugin allows you to execute Nagios check scripts on remote hosts.
* Configure Gogios on both servers to monitor each other using the NRPE plugin. Add a check to the Gogios configuration file (`/etc/gogios.json`) on both servers that uses the NRPE plugin to execute a check script on the other server. For example, if you have Server A and Server B, the configuration on Server A should include a check for Server B, and vice versa.
* Set up alternate cron intervals on both servers. Configure the cron job on Server A to run Gogios at odd minutes (e.g., 1, 3, 5, ...), and on Server B to run at even minutes (e.g., 0, 2, 4, ...). This will ensure that if one server goes down, the other server will continue monitoring and sending notifications.

# But why?

As a Site Reliability Engineer with experience in various monitoring solutions like Nagios, Icinga, Prometheus and OpsGenie, I found that these tools often came with a plethora of features that I didn't necessarily need. Contact groups, host groups, re-check intervals, check clustering, and the requirement of operating a DBMS and a WebUI added complexity and bloat to my monitoring setup.

My primary goal was to have a single email address for notifications and a simple mechanism to periodically execute standard Nagios check scripts and notify me of any state changes. I wanted the most minimalistic monitoring solution possible, but I wasn't satisfied with the available options.

This led me to create Gogios, a lightweight monitoring tool tailored to my specific needs. I chose the Go programming language for this project as it not only allowed me to refresh my Go programming skills but also provided a robust platform for developing a fast and efficient monitoring tool.

Gogios eliminates unnecessary features and focuses on simplicity, providing a no-frills monitoring solution for small-scale self-hosted servers and virtual machines. The result is a tool that is easy to configure, set up, and maintain, ensuring that monitoring your resources is as hassle-free as possible.

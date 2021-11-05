# littlesteward
Stewards little helper

## Functinality

Take a script/program, and SCP it to 1+ nodes.

Connect via SSH, execute the script, and get the result back.

Will retry all failed nodes until successful.

All connection and execution of scripts will be done concurrently.

## Flags

```bash
Usage of littlesteward:
  -idRSAFile string
    the id rsa file to use for ssh authentication
  -script string
    the script to exexute
  -sshUser string
    ssh user id
```

## Files

### hosts.txt

Entries in the host file should be in the format:

```text
192.168.0.1, myhostname1
192.168.0.2, myhostname2
```

Hosts where the script have succeeded successfully will be automatically moved from the hosts.txt file to the done.log file.

### status.log

The status of connecting, execution of scripts, and other will be logged into the status.log file.

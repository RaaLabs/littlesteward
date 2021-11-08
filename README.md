# littlesteward

Stewards little helper

## Functinality

Take a script/program, and SCP it to 1+ nodes.

Connect via SSH, execute the script, and get the result back.

```text
// Basic idea of operation:
// - Will load all the ip adresses and hosts from the hosts.txt file.
// - Will try to copy the script file to the host.
// - Will try to execute the script file on the host.
//
// If successful an entry will be made in the done.log file, followed by the
// output of the script, and the node is removed from the hosts.txt file.
//
// If unsuccessful an entry will be made in the failed.log file, followed by the
// output of the script, and the node is removed from the hosts.txt file.
//
// If unable to initiate connection, an error will be printed to the console,
// and the node name will be kept in the hosts.txt file so it will be retried on
// the next run.
//
// The program will run until the hosts.txt file is empty.
```

The HOSTNAME from `hosts.txt` is exported into the script as an environmental variable upon script execution. This let's you use the shell variable **`$HOSTNAME`** in the script if needed.
Check out example script file `script-test.sh`.

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
192.168.0.1,myhostname1
192.168.0.2,myhostname2
```

Hosts where the script have succeeded successfully will be automatically moved from the hosts.txt file to the done.log file, so the script will not be rerun again on successful nodes.

### failed.log

Nodes where the script failed to be executed, followed by the error message from the script execution.

Failed nodes will also be removed from the `hosts.txt` file.

**NB:** When rerunning on failed nodes, the failed nodes can be copied directly into the `hosts.txt` file when you want to retry. Only the first two **ip** and **hostname** comma separated values are read, and the error message are discarded, so it is not necessary to remove the error message when copying.

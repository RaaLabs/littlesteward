#!/bin/bash

# Script to be copied over to the host, and executed.
# The script should exit with a line echoing out the
# error or success message, and should then be followed
# by an error code 0 if ok, and >0 if not ok.
##
# The HOSTNAME environmental variable is passed on to
# the host when the ssh session are initiated within
# the wrapper script. It is read from the hosts.txt
# file which should be in the format:
# 10.10.10.1,myhostnamehere

## The ${NODENAME} env variable is taken from the nodename part in the hosts.txt file,
## and is exported directly in the ssh command when executed, and can be used within
## the script in the following way.
##
# cat >/etc/hostname <<EOF
# ${NODENAME}
# EOF

## Only the last error is read by the from ssh in the main program, so we can add more
## things to do like for example this
##
# progName="myservice"
# if ! systemctl stop $progName.service >/dev/null 2>&1; then
#     echo "*" error: systemctl stop $progName.service
#     exit 1
# fi

errorMessage=$(
    # Redirect stderr to stdout for all command with the closure.
    {
        swupd info
        # More commands can be added below within the parentheses.
    } 2>&1
)

# ------- We're done, send output back to ssh client

# Check if all went ok with the previous command sequence, or if any errors happened.
# We return any result back to the ssh sessions which called this script by echoing
# the result, and do an exit <error code>.
# We can return one line back, so if more values are needed to be returned, they have
# to be formated into the same line.
#
# All went ok.
if [ $? -eq 0 ]; then
    echo "successfully executed systemctl start wgkeepalive.service : $errorMessage"
    exit 0
# An error happened.
else
    echo "failed executing script: $errorMessage"
    exit 1
fi

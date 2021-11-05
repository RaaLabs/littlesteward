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

errorMessage=$(
    # Redirect stderr to stdout for all command with the closure.
    {
        swupd info
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

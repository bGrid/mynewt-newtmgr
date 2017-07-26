<!--
#
# Licensed to the Apache Software Foundation (ASF) under one
# or more contributor license agreements.  See the NOTICE file
# distributed with this work for additional information
# regarding copyright ownership.  The ASF licenses this file
# to you under the Apache License, Version 2.0 (the
# "License"); you may not use this file except in compliance
# with the License.  You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
#  KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.
#
-->

# Newtmgr

Newt Manager (newtmgr) is the application tool that enables a user to communicate with and manage remote devices running the Mynewt OS. It uses a connection profile to establish a connection with a device and sends command requests to the device.  Thew newtmgr tool is documented at http://mynewt.apache.org/latest/newtmgr/overview/

### Vendoring

newtmgr is vendored using the godep tool (https://github.com/tools/godep).  Two dependencies are intentially excluded from the vendor directory:

* https://github.com/currantlabs/ble
* https://github.com/raff/goble

The reason these are not vendored is to prevent errors during elicited by `go get mynewt.apache.org/newtmgr/...` on linux.  These two dependencies contain OS-specific code, something `go get` seems to trip over when it is vendored.

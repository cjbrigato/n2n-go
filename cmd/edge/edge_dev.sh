#!/bin/bash

go build && sudo ./edge -community acme -supernode 192.168.1.253:7777

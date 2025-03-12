#!/bin/bash

go build && sudo ./edge -community acme -id $(hostname) -supernode 192.168.1.253:7777

#!/bin/bash

go build && ./edge -community acme -id $(hostname) -supernode 192.168.1.253:7777
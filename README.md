# Demeter
A tool for bulk programing GV UCP cards.

## Usage
For Demeter to work, you must have rolltrak installed (install rollsuite) and rolltrak must be added to your windows system path.

`C:\Program Files (x86)\SAM\RollCallSuite\RollTrak.exe`
https://www.architectryan.com/2018/03/17/add-to-the-path-on-windows-10/

Create a "Group" and set desired properties for a group of UCP/IQ frames.
Add a frame via it's IP address and assign it to a group.

Then set frame to "scan" to discover cards and settings. Or, "scan & blast" to apply settings.

When the frame is set to "Scan & Blast" it will use rolltrak to connect to the frame and will apply settings based on the group.
Per card overrides can be applied once a card has been discovered.

The system will connect to a card via the frame to find it's IP addresses and will use the frame controler to change IP settings.
If it cannot reach the card via the cards IP it will not show any settings others than the IP settings and will not attempt to change them.
Once the card is reachable it will get and set settings directly to the card.

## Dev
This is an electron/typescript program (GO re-write on pause...)
Download source, "npm install" (or yarn or any other packagemanager of your choice)
To run in dev, "npm start"
To build, first comiple the typescript to js, "npm run compile"
Then build "npm run build"

## Thing's we've noticed

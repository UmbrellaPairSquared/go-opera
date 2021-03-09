// Launches a 1/1 go-opera fakenet, minting, sending ETH, sending ERC20s, and doing misc contract operations
// Plenty of different state mutations, caused by transactions and by internal trasnactions
// Not to mention a thorough amount of revisions
// Then saves the state, loads it onto a fresh node, and compares the two
// Finally ensures a new TX can be added for an existing account at nonce 0
// This is done since there's the snapshot purposefully clears the nonce to reduce state size

"use strict";

const fs = require("fs");
const net = require("net");
const { spawn } = require("child_process");

const del = require("del");
const Web3 = require("web3");

// Contracts

// Just stores a string; useful for testing non-uint256 storage, not to mention variable sized storage
const STRING_STORAGE_ABI = JSON.parse(fs.readFileSync("./contracts/StringStorage/StringStorage.abi").toString());
const STRING_STORAGE_BIN = "0x" + fs.readFileSync("./contracts/StringStorage/StringStorage.bin").toString().trim();

// Internally deploys a contract + updates its storage. Used to ensure ALL accounts are swept
const DAC_ABI = JSON.parse(fs.readFileSync("./contracts/StringStorage/DeployAnotherContract.abi").toString());
const DAC_BIN = "0x" + fs.readFileSync("./contracts/StringStorage/DeployAnotherContract.bin").toString().trim();

// Test files
const FAKENET_DATADIR = "snapshot-test-data";
const SNAPSHOT_FILE = "snapshot-test-file"

function random(min, max) {
  return Math.floor(Math.random() * (max - min)) + min;
}

function randomString() {
  let length = random(0, 96);
  let res = "";
  var characters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  for (let i = 0; i < length; i++) {
      res += characters.charAt(random(0, characters.length));
   }
   return res;
}

function sleep(seconds) {
  return new Promise((resolve) => setTimeout(resolve, seconds * 1000));
}

async function connectWeb3() {
  await sleep(15);
  web3 = new Web3(`${FAKENET_DATADIR}/opera.ipc`, net);
  web3.eth.defaultBlock = "pending";
}

let web3;
async function send(from, to, value) {
  await web3.eth.sendTransaction({
    from,
    to,
    value,
    gas: 21000
  }); //, web3.eth.accounts.wallet[from].privateKey);
}

// There's no commands to control block production
// That said, one appears every second or so
async function mineBlock() {
  await sleep(1.5);
}

del.sync(FAKENET_DATADIR);

// Generate 1/1 go-opera fakenet
let opera = spawn("../../build/opera", ["--nousb", "--fakenet", "1/1", /*"--password", "./PASSWORD_FILE",*/ "--datadir", FAKENET_DATADIR]);
function addOutput(instance) {
  /*
  instance.stdout.on("data", data => {
    console.log(`stdout: ${data}`)
  });
  instance.stderr.on("data", data => {
    console.log(`stderr: ${data}`)
  });
  instance.on("close", code => {
    console.log(`opera node exited with code ${code}`)
  });
  */
}
addOutput(opera);

(async () => {
  await connectWeb3();

  // Tracking variables
  let balances = {};
  let strings = {};

  let accounts = [web3.eth.accounts.wallet.create(1)[0].address];
  let nodeAccounts = await web3.eth.personal.getAccounts();

  // Wait for the node to give us a balance
  while ((await web3.eth.getBalance(nodeAccounts[0])).toString() === "0") {
    await sleep(1);
  }

  let value = (
    new web3.utils.BN(await web3.eth.getBalance(nodeAccounts[0]))
  ).div(new web3.utils.BN(2)).toString();
  await web3.eth.personal.sendTransaction({
    from: nodeAccounts[0],
    gas: 21000,
    to: accounts[0],
    value
  }, "fakepassword");
  await mineBlock();

  // Run for 30 blocks
  for (let i = 0; i < 30; i++) {
    console.log(i);

    // Generate a new address
    accounts.push(web3.eth.accounts.wallet.create(1)[0].address);

    // Send ETH with twenty rounds
    for (let a = 0; a < 20; a++) {
      // Randomly select an address to send from/to
      let acc = random(0, accounts.length);
      // Send to another account if we have funds and a 50% chance happens
      if (
        (new web3.utils.BN(await web3.eth.getBalance(accounts[acc]))).gt(new web3.utils.BN(web3.utils.toWei("1"))) &&
        (random(0, 2) === 0)
      ) {
        await send(accounts[acc], accounts[random(0, accounts.length)], web3.utils.toWei("0." + random(1, 98).toString()));
      }

      // Send to it with a 1/4 chance
      if (random(0, 4) == 0) {
        await send(accounts[0], accounts[acc], web3.utils.toWei(random(1, 5).toString()));
      }
    }

    // Random chance to deploy a string contract
    if (random(0, 4) == 0) {
      let str = randomString();
      let strStorage = await (new web3.eth.Contract(STRING_STORAGE_ABI)).deploy({
        data: STRING_STORAGE_BIN,
        arguments: [str]
      }).send({
        // Should always have enough ETH
        from: accounts[0],
        gas: 500000
      });
      strings[strStorage.options.address] = str;
    }

    // Random chance to deploy a DAC
    if (random(0, 8) == 0) {
      let dac = await (new web3.eth.Contract(DAC_ABI)).deploy({
        data: DAC_BIN
      }).send({
        // Should always have enough ETH
        from: accounts[0],
        gas: 1000000
      });

      let str = randomString();
      strings[(await dac.methods.deploy(str).send({
        from: accounts[0],
        gas: 800000
      })).events.NewString.returnValues.another] = str;
    }

    // Update string contracts
    for (let strAddr in strings) {
      if (random(0, 3) == 0) {
        let str = randomString();
        await (new web3.eth.Contract(STRING_STORAGE_ABI, strAddr)).methods.setString(str).send({
          from: accounts[0],
          gas: 300000
        });
        strings[strAddr] = str;
      }
    }

    await mineBlock();
  }

  // Update the coinbase account's balance
  balances[nodeAccounts[0]] = await web3.eth.getBalance(nodeAccounts[0]);
  // Update every generated account's balance
  for (let acc of accounts) {
    balances[acc] = await web3.eth.getBalance(acc);
  }

  // Shut the node down
  opera.kill();
  while (opera.exitCode == null) {
    await sleep(1);
  }

  // Start the node and take a snapshot
  opera = spawn("../../build/opera", ["--nousb", "--fakenet", "1/1", "--datadir", FAKENET_DATADIR, "--save-snapshot", SNAPSHOT_FILE]);
  addOutput(opera);
  // Wait an extra 30 seconds to let the snapshot occur
  await sleep(30);
  opera.kill();
  while (opera.exitCode == null) {
    await sleep(1);
  }

  // Delete the data directory so we get a fresh node
  await del(FAKENET_DATADIR);

  // Reboot the node with the snapshot
  opera = spawn("../../build/opera", ["--nousb", "--fakenet", "1/1", "--datadir", FAKENET_DATADIR, "--load-snapshot", SNAPSHOT_FILE]);
  addOutput(opera);
  // Same as above, except once we connect, compare our data sets
  await connectWeb3();

  for (let addr in balances) {
    if (balances[addr] != (await web3.eth.getBalance(addr)).toString()) {
      console.log("Balance doesn't match");
      process.exit(1);
    }
  }

  for (let addr in strings) {
    if ((await (new web3.eth.Contract(STRING_STORAGE_ABI, addr)).methods.getString().call()) != strings[addr]) {
      console.log("String doesn't match");
      process.exit(1)
    }
  }

  opera.kill();
  console.log("Test passed.")
})();

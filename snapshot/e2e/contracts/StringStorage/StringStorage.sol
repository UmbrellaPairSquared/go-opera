// SPDX-License-Identifier: UNLICENSED

pragma solidity >=0.8.0;

contract StringStorage {
  string stringStorage;

  constructor(string memory data) {
    stringStorage = data;
  }

  function setString(string memory data) external {
    stringStorage = data;
  }

  function getString() external view returns (string memory) {
    return stringStorage;
  }
}

contract DeployAnotherContract {
  event NewString(address indexed another);
  function deploy(string memory data) external {
    StringStorage another = new StringStorage("");
    another.setString(data);
    emit NewString(address(another));
  }
}

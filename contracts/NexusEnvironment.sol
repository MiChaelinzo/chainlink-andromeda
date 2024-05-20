pragma solidity ^0.8.0;

// Interface for the Alien Environment Data oracle
interface AlienEnvironmentData {
  function getAtmosphericData(string memory planetName) external returns (uint pressure, uint temperature, uint windSpeed);
  function getGeologicalData(string memory planetName) external returns (string memory terrainType, string memory resourceDeposit);
}

contract NexusEnvironment {
  AlienEnvironmentData public oracle;
  mapping(string => EnvironmentData) public planetData; // Stores retrieved data for each planet
  enum AccessLevel { Tourist, Researcher, Administrator } // Access levels for users

  struct EnvironmentData {
    uint pressure;
    uint temperature;
    uint windSpeed;
    string terrainType;
    string resourceDeposit;
  }

  mapping(address => AccessLevel) public userAccess; // Tracks user access levels

  // Constructor to set the oracle address
  constructor(address _oracle) {
    oracle = AlienEnvironmentData(_oracle);
  }

  // Function to request and store environment data for a planet
  function updatePlanetData(string memory planetName) public {
    (uint pressure, uint temperature, uint windSpeed) = oracle.getAtmosphericData(planetName);
    (string memory terrainType, string memory resourceDeposit) = oracle.getGeologicalData(planetName);
    planetData[planetName] = EnvironmentData(pressure, temperature, windSpeed, terrainType, resourceDeposit);
  }

  // Function to access environment data for a planet (requires access)
  function getPlanetData(string memory planetName) public view returns (EnvironmentData memory) {
    require(hasAccess(planetName, msg.sender), "Unauthorized access");
    return planetData[planetName];
  }

  // Function to grant user access to a specific planet (restricted to admins)
  function grantAccess(address user, string memory planetName, AccessLevel level) public {
    require(userAccess[msg.sender] == AccessLevel.Administrator, "Only admins can grant access");
    userAccess[user] = level;
  }

  // Function to check user's access level for a specific planet
  function hasAccess(string memory planetName, address user) public view returns (bool) {
    AccessLevel requiredLevel = AccessLevel.Tourist; // Default access level for new planets

    // Check if planet data exists and has a specific access requirement
    if (planetData[planetName].terrainType != "") {
      // Define access level based on terrain type (example logic)
      if (planetData[planetName].terrainType == "Hostile") {
        requiredLevel = AccessLevel.Researcher;
      }
    }

    return userAccess[user] >= requiredLevel;
  }

  // ... other functions to utilize retrieved data for XR/MR environment
}

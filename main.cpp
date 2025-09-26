#include <iostream>
#include <thread>
#include <chrono>
#include "librmcs.h"

int main() {
    std::cout << "=== RMCS C++ Example ===" << std::endl;

    // Optional: Set log file
    // RMCSSetLogFile("rmcs_log.txt");

    // Initialize RMCS (starts WebRTC and MQTT)
    std::cout << "Initializing RMCS..." << std::endl;
    int result = RMCSInit();
    if (result != 0) {
        std::cerr << "Failed to initialize RMCS. Error code: " << result << std::endl;
        return 1;
    }

    std::cout << "RMCS initialized successfully!" << std::endl;

    // Check status
    int status = RMCSGetStatus();
    std::cout << "RMCS Status: " << (status == 1 ? "Running" : "Not Running") << std::endl;

    // Keep running (in real application, you'd have your main loop here)
    std::cout << "\nRMCS is running. Press Enter to stop..." << std::endl;
    std::cin.get();

    // Stop RMCS
    std::cout << "Stopping RMCS..." << std::endl;
    RMCSStop();

    std::cout << "RMCS stopped. Goodbye!" << std::endl;

    return 0;
}
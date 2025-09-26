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

    // Example: Switch cameras programmatically
    std::cout << "\nCamera switching example:" << std::endl;

    // Wait a moment for WebRTC connection to establish
    std::this_thread::sleep_for(std::chrono::seconds(5));

    // // Switch to camera 2
    // std::cout << "Switching to camera 2..." << std::endl;
    // result = RMCSSwitchCamera(2);
    // if (result == 0) {
    //     std::cout << "Successfully switched to camera 2" << std::endl;
    // } else {
    //     std::cerr << "Failed to switch camera. Error code: " << result << std::endl;
    // }

    // std::this_thread::sleep_for(std::chrono::seconds(3));

    // // Switch to camera 5
    // std::cout << "Switching to camera 5..." << std::endl;
    // result = RMCSSwitchCamera(5);
    // if (result == 0) {
    //     std::cout << "Successfully switched to camera 5" << std::endl;
    // } else {
    //     std::cerr << "Failed to switch camera. Error code: " << result << std::endl;
    // }

    // Keep running (in real application, you'd have your main loop here)
    std::cout << "\nRMCS is running. Press Enter to stop..." << std::endl;
    std::cin.get();

    // Stop RMCS
    std::cout << "Stopping RMCS..." << std::endl;
    RMCSStop();

    std::cout << "RMCS stopped. Goodbye!" << std::endl;

    return 0;
}
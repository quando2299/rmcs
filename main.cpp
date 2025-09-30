#include <iostream>
#include <thread>
#include <chrono>
#include <csignal>
#include <atomic>
#include "librmcs.h"

std::atomic<bool> running(true);

void signalHandler(int signum) {
    std::cout << "\nReceived signal " << signum << ". Shutting down..." << std::endl;
    running = false;

    // Restore default signal handler to allow force quit on second Ctrl+C
    signal(SIGINT, SIG_DFL);
    signal(SIGTERM, SIG_DFL);
}

int main() {
    std::cout << "=== RMCS C++ Example ===" << std::endl;

    // Setup signal handlers for graceful shutdown
    signal(SIGINT, signalHandler);   // Ctrl+C
    signal(SIGTERM, signalHandler);  // Docker stop

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

    // Keep running until signal received
    std::cout << "\nRMCS is running. Press Ctrl+C to stop..." << std::endl;
    while (running) {
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    // Stop RMCS
    std::cout << "Stopping RMCS..." << std::endl;
    RMCSStop();

    std::cout << "RMCS stopped. Goodbye!" << std::endl;

    return 0;
}
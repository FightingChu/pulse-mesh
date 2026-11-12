cmake_minimum_required(VERSION 3.16)
project(pulse_firmware CXX)
set(CMAKE_CXX_STANDARD 17)
enable_testing()

add_executable(pulse_firmware src/main.cpp src/sensor.cpp)
target_include_directories(pulse_firmware PRIVATE include)
target_compile_options(pulse_firmware PRIVATE $<$<CXX_COMPILER_ID:GNU,Clang>:-Wall;-Wextra;-Werror>)

add_executable(pulse_firmware_test test/test_sensor.cpp src/sensor.cpp)
target_include_directories(pulse_firmware_test PRIVATE include)
target_compile_options(pulse_firmware_test PRIVATE $<$<CXX_COMPILER_ID:GNU,Clang>:-Wall;-Wextra;-Werror>)
add_test(NAME sensor_spectrum COMMAND pulse_firmware_test)

if(MINGW)
  target_link_options(pulse_firmware PRIVATE -static)
  target_link_options(pulse_firmware_test PRIVATE -static)
endif()

FROM gcc:latest

# Cài `time` để đo thời gian chạy
RUN apt-get update && apt-get install -y time

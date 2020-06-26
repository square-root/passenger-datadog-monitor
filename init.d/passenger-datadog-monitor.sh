#!/bin/bash
#
# passenger-datadog-monitor
#
# chkconfig: - 99 01
# description: send metrics from Passenger to Datadog

# Source function library.
. /etc/init.d/functions

RETVAL=0
PDM="passenger-datadog-monitor"
PDMDIR="/usr/local/bin"
PIDFILE="/var/run/${PDM}.pid"
LOGFILE="/tmp/passenger-datadog-monitor.log"
SLEEPCOUNT="11"

################################################################################
# Start the service
# Globals:
#   LOGFILE
#   PDM
#   PDMDIR
#   PIDFILE
#   SLEEPTIME
#   SLEEPCOUNT
# Arguments:
#   None
# Returns:
#   RETVAL
################################################################################
start() {
  PIDS=( $(get_pid) )
  if [[ "${#PIDS[@]}" -gt 0 ]]; then
    printf "%s\n" "Can't start; ${PDM} already running"
    exit 1
  fi
  printf "%s\n" "Starting ${PDM}: "
  ## 'su' needed here b/c we use RVM
  su - root -c "/usr/bin/nohup ${PDMDIR}/${PDM} >${LOGFILE} 2>&1 &"
  if [[ "${RETVAL}" -eq 0 ]]; then
  while true; do
    PIDS=( $(get_pid) )
    if [[ "${#PIDS[@]}" -gt 1 ]]; then
      if [[ "${SLEEPCOUNT}" -eq 0 ]]; then
        echo "ERROR: Expected 1 process ID for ${PDM} but found ${#PIDS[@]}."
        echo "Process ID's:"
        echo "${PIDS[@]}"
        exit 1
      fi
      sleep .5
      (( SLEEPCOUNT-- ))
    else
      echo "${PIDS[@]}" >"${PIDFILE}"
      break
    fi
  done
    printf "%s\n" "Ok"
  fi
  return "${RETVAL}"
}

################################################################################
# Stop the service
# Globals:
#   PDM
#   PIDFILE
# Arguments:
#   None
# Returns:
#   None
################################################################################
stop() {
  echo -n "Shutting down ${PDM}: "
  if [[ -f "${PIDFILE}" ]]; then
    kill -9 "$(cat ${PIDFILE})"
    printf "%s\n" "Ok"
    rm -f "${PIDFILE}"
  else
    printf "%s\n" "pidfile not found"
  fi
}

################################################################################
# Print service status
# Globals:
#   PDM
#   PIDFILE
# Arguments:
#   None
# Returns:
#   None
################################################################################
status() {
  echo -n "Checking ${PDM} status: "
  if [[ -f "${PIDFILE}" ]]; then
    PID="$(cat ${PIDFILE})"
    if [[ -z "$(ps axf | grep ${PID} | grep -v grep)" ]]; then
      printf "%s\n" "${PDM} dead but pidfile exists"
      exit 1
    else
      echo "${PDM} running, process ID: ${PID}"
    fi
  else
    printf "%s\n" "${PDM} not running"
    exit 3
  fi
}

################################################################################
# Get the process ID of the service
# Globals:
#   PDMDIR
#   PDM
# Arguments:
#   None
# Returns:
#   PID
################################################################################
get_pid() {
  mapfile -t PID < <(pgrep -f -x "${PDMDIR}/${PDM}")
  echo "${PID[@]}"
}

main() {
  case $1 in
    start)
      start
      ;;
    stop)
      stop
      ;;
    status)
      status
      ;;
    restart)
      stop
      start
      ;;
    *)
      echo "Usage: ${PDM} {start|stop|status|restart}"
      exit 1
      ;;
  esac
  exit "${RETVAL}"
}

main "$@"

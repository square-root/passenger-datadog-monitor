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
SLEEPTIME=".5" # 1/2 second
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
  if [[ -n "$(get_pid)" ]]; then
    printf "%s\n" "Can't start; ${PDM} already running"
    exit 1
  fi
  printf "%s\n" "Starting ${PDM}: "
  ## 'su' needed here b/c we use RVM
  su - root -c "/usr/bin/nohup ${PDMDIR}/${PDM} > ${LOGFILE} 2>&1 &"
  if [[ "${RETVAL}" -eq 0 ]]; then
    PID="$(get_pid)"
    echo "${PID}" > "${PIDFILE}"
    PIDCOUNT="$(wc -l ${PIDFILE} | awk '{print $1}')"
    while [[ "${PIDCOUNT}" -ne 1 ]]; do
      sleep "${SLEEPTIME}"
      (( SLEEPCOUNT-- ))
      if [[ "${SLEEPCOUNT}" -eq 0 ]]; then
        echo "There are multiple ${PDM} processes running, please check."
        exit 1
      fi
      PID="$(get_pid)"
      echo "${PID}" > "${PIDFILE}"
      PIDCOUNT="$(wc -l ${PIDFILE} | awk '{print $1}')"
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
      echo "${PDM} Running"
    fi
  else
    printf "%s\n" "${PDM} not running"
    exit 3
  fi
}

################################################################################
# Get the process ID of the service
# Globals:
#   PDM
#   PIDFILE
# Arguments:
#   None
# Returns:
#   None
################################################################################
get_pid() {
  pgrep -f -x "${PDMDIR}/${PDM}"
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

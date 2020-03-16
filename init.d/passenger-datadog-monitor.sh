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
PIDFILE=/var/run/${PDM}.pid
LOGFILE="/tmp/passenger-datadog-monitor.log"


start() {
  if [[ -n "$(get_pid)" ]] ; then
    printf "%s\n" "Can't start; ${PDM} already running"
    exit 1
  fi
  printf "%s\n" "Starting ${PDM}: "
  ## 'su' needed here b/c we use RVM
  su - root -c "/usr/bin/nohup ${PDMDIR}/${PDM} > ${LOGFILE} 2>&1 &"
  if [[ ${RETVAL} -eq 0 ]]; then
    PID=$(get_pid)
    echo "${PID}" > ${PIDFILE}
    printf "%s\n" "Ok"
  fi
  return ${RETVAL}
}

stop() {
  echo -n "Shutting down ${PDM}: "
  if [[ -f ${PIDFILE} ]]; then
    kill -9 "$(cat ${PIDFILE})"
    printf "%s\n" "Ok"
    rm -f ${PIDFILE}
  else
    printf "%s\n" "pidfile not found"
  fi
}

status() {
  echo -n "Checking ${PDM} status: "
  if [[ -f ${PIDFILE} ]]; then
    PID=$(cat ${PIDFILE})
    if [[ -z "$(ps axf | grep ${PID} | grep -v grep)" ]]; then
      printf "%s\n" "${PDM} dead but pidfile exists"
    else
      echo "${PDM} Running"
    fi
  else
    printf "%s\n" "${PDM} not running"
  fi
}

get_pid() {
  pgrep -f -x "${PDMDIR}/${PDM}"
}

case "$1" in
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
exit ${RETVAL}

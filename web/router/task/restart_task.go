package task

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nange/gospider/common"
	"github.com/nange/gospider/spider"
	"github.com/nange/gospider/web/core"
	"github.com/nange/gospider/web/model"
	"github.com/nange/gospider/web/service"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// 根据任务id重启定时任务
func RestartTask(c *gin.Context) {
	taskIDStr := c.Param("id")
	if taskIDStr == "" {
		logrus.Warnf("RestartTaskReq taskID is empty")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	taskID, err := strconv.ParseUint(taskIDStr, 10, 64)
	if err != nil {
		logrus.Warnf("RestartTaskReq taskID format is invalid, taskID:%v", taskIDStr)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	logrus.Infof("RestartTaskReq:%+v", taskID)

	if taskLock.IsRunning(taskID) {
		c.String(http.StatusConflict, "任务正在执行")
		return
	}
	defer taskLock.Complete(taskID)

	// query task info from db
	task := &model.Task{}
	err = model.NewTaskQuerySet(core.GetDB()).IDEq(taskID).One(task)
	if err != nil {
		logrus.Errorf("RestartTaskReq query model task fail, taskID: %v , err: %+v", taskIDStr, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	// only allow crontab task
	if task.CronSpec == "" {
		logrus.Warnf("RestartTaskReq taskID is not crontab task, taskID: %v", taskIDStr)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// check task status
	if !taskCanBeRestart(task) {
		logrus.Warnf("StartTaskReq taskID status is non-conformance , taskID: %v", taskIDStr)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// create crontab task
	err = service.RestartTask(*task)
	if err != nil {
		logrus.Errorf("RestartTaskReq run task fail, taskID: %v , err: %+v", taskIDStr, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	// update task status
	err = model.NewTaskQuerySet(core.GetDB()).IDEq(taskID).GetUpdater().SetStatus(common.TaskStatusCompleted).Update()
	if err != nil {
		// stop cron task
		if ct := service.GetCronTask(taskID); ct != nil {
			ct.Stop()
		}

		// cancel spider task
		if err := spider.CancelTask(taskID); err != nil {
			logrus.Warnf("spider.CancelTask err:%v", err)
		}

		logrus.Errorf("RestartTaskReq update task status err:%+v", errors.WithStack(err))
		c.String(http.StatusInternalServerError, "")
		return
	}
	c.String(http.StatusOK, "success")
}

func taskCanBeRestart(task *model.Task) bool {
	return task.Status == common.TaskStatusStopped
}

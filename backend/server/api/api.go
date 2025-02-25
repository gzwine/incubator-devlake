/*
Licensed to the Apache Software Foundation (ASF) under one or more
contributor license agreements.  See the NOTICE file distributed with
this work for additional information regarding copyright ownership.
The ASF licenses this file to You under the Apache License, Version 2.0
(the "License"); you may not use this file except in compliance with
the License.  You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apache/incubator-devlake/core/config"
	"github.com/apache/incubator-devlake/core/errors"
	"github.com/apache/incubator-devlake/impls/logruslog"
	_ "github.com/apache/incubator-devlake/server/api/docs"
	"github.com/apache/incubator-devlake/server/api/remote"
	"github.com/apache/incubator-devlake/server/api/shared"
	"github.com/apache/incubator-devlake/server/services"
	"github.com/apache/incubator-devlake/server/services/remote/bridge"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/swaggo/gin-swagger/swaggerFiles"
)

const DB_MIGRATION_REQUIRED = `
New migration scripts detected. Database migration is required to launch DevLake.
WARNING: Performing migration may wipe collected data for consistency and re-collecting data may be required.
To proceed, please send a request to <config-ui-endpoint>/api/proceed-db-migration (or <devlake-endpoint>/proceed-db-migration).
Alternatively, you may downgrade back to the previous DevLake version.
`

// @title  DevLake Swagger API
// @version 0.1
// @description  <h2>This is the main page of devlake api</h2>
// @license.name Apache-2.0
// @host localhost:8080
// @BasePath /
func CreateApiService() {
	services.Init()
	v := config.GetConfig()
	gin.SetMode(v.GetString("MODE"))
	router := gin.Default()
	remotePluginsEnabled := v.GetBool("ENABLE_REMOTE_PLUGINS")
	if remotePluginsEnabled {
		router.POST("/plugins/register", remote.RegisterPlugin(router, registerPluginEndpoints))
	}
	// Wait for user confirmation if db migration is needed
	router.GET("/proceed-db-migration", func(ctx *gin.Context) {
		if !services.MigrationRequireConfirmation() {
			shared.ApiOutputSuccess(ctx, nil, http.StatusOK)
			return
		}
		err := services.ExecuteMigration()
		if err != nil {
			shared.ApiOutputError(ctx, errors.Default.Wrap(err, "error executing migration"))
			return
		}
		shared.ApiOutputSuccess(ctx, nil, http.StatusOK)
	})
	router.Use(func(ctx *gin.Context) {
		if !services.MigrationRequireConfirmation() {
			return
		}
		shared.ApiOutputError(
			ctx,
			errors.HttpStatus(http.StatusPreconditionRequired).New(DB_MIGRATION_REQUIRED),
		)
		ctx.Abort()
	})

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	//endpoint debug log
	gin.DebugPrintRouteFunc = func(httpMethod, absolutePath, handlerName string, nuHandlers int) {
		logruslog.Global.Printf("endpoint %v %v %v %v", httpMethod, absolutePath, handlerName, nuHandlers)
	}

	// CORS CONFIG
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"PUT", "PATCH", "POST", "GET", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           120 * time.Hour,
	}))

	RegisterRouter(router)
	port := v.GetString("PORT")
	if remotePluginsEnabled {
		go bootstrapRemotePlugins(v)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		panic(fmt.Errorf("PORT [%s] must be int: %s", port, err.Error()))
	}

	err = router.Run(fmt.Sprintf("0.0.0.0:%d", portNum))
	if err != nil {
		panic(err)
	}
}

func bootstrapRemotePlugins(v *viper.Viper) {
	port := v.GetString("PORT")
	port = strings.TrimLeft(port, ":")
	portNum, err := strconv.Atoi(port)
	if err != nil {
		panic(fmt.Errorf("PORT [%s] must be int: %s", port, err.Error()))
	}
	err = bridge.Bootstrap(v, portNum)
	if err != nil {
		logruslog.Global.Error(err, "")
	}
}
